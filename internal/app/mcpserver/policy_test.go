package mcpserver

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/damonto/sigmo/internal/app/mcpauth"
)

type guardedTestInput struct {
	Value          string `json:"value"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type guardedTestOutput struct {
	Value string `json:"value"`
}

func guardedTestPolicy() GuardedToolPolicy[guardedTestInput] {
	return GuardedToolPolicy[guardedTestInput]{
		Confirmation:   func(guardedTestInput) string { return "Confirm test operation" },
		IdempotencyKey: func(input guardedTestInput) string { return input.IdempotencyKey },
	}
}

func acceptingExecution() *execution {
	return &execution{policy: executionPolicyState{
		confirm: func(context.Context, *mcp.CallToolRequest, string) error { return nil },
	}}
}

func TestGuardedToolIdempotency(t *testing.T) {
	var calls atomic.Int32
	exec := acceptingExecution()
	policy := guardedTestPolicy()
	handler := func(context.Context, *mcp.CallToolRequest, mcpauth.Grant, guardedTestInput) (guardedTestOutput, error) {
		calls.Add(1)
		return guardedTestOutput{Value: "done"}, nil
	}
	input := guardedTestInput{Value: "same", IdempotencyKey: "request-1"}

	for range 2 {
		out, err := executeGuardedTool(context.Background(), nil, exec, "test_tool", policy, handler, input)
		if err != nil {
			t.Fatalf("executeGuardedTool() error = %v", err)
		}
		if out.Value != "done" {
			t.Fatalf("executeGuardedTool() output = %+v", out)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
}

func TestGuardedToolConcurrentIdempotency(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	exec := acceptingExecution()
	policy := guardedTestPolicy()
	handler := func(context.Context, *mcp.CallToolRequest, mcpauth.Grant, guardedTestInput) (guardedTestOutput, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return guardedTestOutput{Value: "done"}, nil
	}
	input := guardedTestInput{Value: "same", IdempotencyKey: "request-1"}
	results := make(chan error, 2)
	call := func() {
		out, err := executeGuardedTool(context.Background(), nil, exec, "test_tool", policy, handler, input)
		if err == nil && out.Value != "done" {
			err = errors.New("unexpected guarded tool output")
		}
		results <- err
	}

	go call()
	<-started
	go call()
	select {
	case err := <-results:
		t.Fatalf("duplicate call returned before the original completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("executeGuardedTool() error = %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
}

func TestGuardedToolIdempotencyConflict(t *testing.T) {
	var calls atomic.Int32
	exec := acceptingExecution()
	policy := guardedTestPolicy()
	handler := func(_ context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input guardedTestInput) (guardedTestOutput, error) {
		calls.Add(1)
		return guardedTestOutput{Value: input.Value}, nil
	}

	_, err := executeGuardedTool(context.Background(), nil, exec, "test_tool", policy, handler, guardedTestInput{Value: "first", IdempotencyKey: "request-1"})
	if err != nil {
		t.Fatalf("first executeGuardedTool() error = %v", err)
	}
	_, err = executeGuardedTool(context.Background(), nil, exec, "test_tool", policy, handler, guardedTestInput{Value: "second", IdempotencyKey: "request-1"})
	if got := ErrorCode(err); got != "idempotency_conflict" {
		t.Fatalf("second executeGuardedTool() error code = %q, want idempotency_conflict; error = %v", got, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
}

func TestGuardedToolIdempotencyKeyValidation(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "missing"},
		{name: "whitespace", key: "  "},
		{name: "too long", key: strings.Repeat("a", 129)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executeGuardedTool(
				context.Background(), nil, acceptingExecution(), "test_tool", guardedTestPolicy(),
				func(context.Context, *mcp.CallToolRequest, mcpauth.Grant, guardedTestInput) (guardedTestOutput, error) {
					t.Fatal("handler called with an invalid idempotency key")
					return guardedTestOutput{}, nil
				},
				guardedTestInput{Value: "test", IdempotencyKey: tt.key},
			)
			if got := ErrorCode(err); got != "invalid_request" {
				t.Fatalf("executeGuardedTool() error code = %q, want invalid_request; error = %v", got, err)
			}
		})
	}
}

func TestGuardedToolIdempotencyExpires(t *testing.T) {
	now := time.Date(2026, time.July, 19, 0, 0, 0, 0, time.UTC)
	exec := acceptingExecution()
	exec.policy.now = func() time.Time { return now }
	var calls atomic.Int32
	handler := func(context.Context, *mcp.CallToolRequest, mcpauth.Grant, guardedTestInput) (guardedTestOutput, error) {
		calls.Add(1)
		return guardedTestOutput{Value: "done"}, nil
	}
	input := guardedTestInput{Value: "same", IdempotencyKey: "request-1"}

	for range 2 {
		if _, err := executeGuardedTool(context.Background(), nil, exec, "test_tool", guardedTestPolicy(), handler, input); err != nil {
			t.Fatalf("executeGuardedTool() error = %v", err)
		}
		now = now.Add(idempotencyRetention)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("handler calls = %d, want 2", got)
	}
}

func TestGuardedToolPreflightFailureIsNotCached(t *testing.T) {
	var validations atomic.Int32
	var calls atomic.Int32
	exec := acceptingExecution()
	policy := guardedTestPolicy()
	policy.Validate = func(context.Context, guardedTestInput) error {
		if validations.Add(1) == 1 {
			return NewToolError("invalid_request", "temporary validation failure", nil)
		}
		return nil
	}
	handler := func(context.Context, *mcp.CallToolRequest, mcpauth.Grant, guardedTestInput) (guardedTestOutput, error) {
		calls.Add(1)
		return guardedTestOutput{Value: "done"}, nil
	}
	input := guardedTestInput{Value: "same", IdempotencyKey: "request-1"}

	if _, err := executeGuardedTool(context.Background(), nil, exec, "test_tool", policy, handler, input); ErrorCode(err) != "invalid_request" {
		t.Fatalf("first executeGuardedTool() error = %v", err)
	}
	if _, err := executeGuardedTool(context.Background(), nil, exec, "test_tool", policy, handler, input); err != nil {
		t.Fatalf("second executeGuardedTool() error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
}

func TestGuardedToolHandlerErrorIsCached(t *testing.T) {
	errHandler := errors.New("handler error")
	var calls atomic.Int32
	exec := acceptingExecution()
	handler := func(context.Context, *mcp.CallToolRequest, mcpauth.Grant, guardedTestInput) (guardedTestOutput, error) {
		calls.Add(1)
		return guardedTestOutput{}, errHandler
	}
	input := guardedTestInput{Value: "same", IdempotencyKey: "request-1"}

	for range 2 {
		_, err := executeGuardedTool(context.Background(), nil, exec, "test_tool", guardedTestPolicy(), handler, input)
		if !errors.Is(err, errHandler) {
			t.Fatalf("executeGuardedTool() error = %v, want handler error", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
}

func TestGuardedToolPanicIsNotCached(t *testing.T) {
	var calls atomic.Int32
	exec := acceptingExecution()
	handler := func(context.Context, *mcp.CallToolRequest, mcpauth.Grant, guardedTestInput) (guardedTestOutput, error) {
		if calls.Add(1) == 1 {
			panic("test panic")
		}
		return guardedTestOutput{Value: "done"}, nil
	}
	input := guardedTestInput{Value: "same", IdempotencyKey: "request-1"}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("executeGuardedTool() did not propagate handler panic")
			}
		}()
		_, _ = executeGuardedTool(context.Background(), nil, exec, "test_tool", guardedTestPolicy(), handler, input)
	}()
	out, err := executeGuardedTool(context.Background(), nil, exec, "test_tool", guardedTestPolicy(), handler, input)
	if err != nil {
		t.Fatalf("second executeGuardedTool() error = %v", err)
	}
	if out.Value != "done" {
		t.Fatalf("second executeGuardedTool() output = %+v", out)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("handler calls = %d, want 2", got)
	}
}

func TestExecutionPolicyRateLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit RateLimit
	}{
		{name: "SMS", limit: RateLimit{Requests: 10, Window: time.Minute}},
		{name: "USSD", limit: RateLimit{Requests: 5, Window: time.Minute}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, time.July, 19, 0, 0, 0, 0, time.UTC)
			state := executionPolicyState{now: func() time.Time { return now }}
			for range tt.limit.Requests {
				if err := state.checkRate(tt.name, tt.limit); err != nil {
					t.Fatalf("checkRate() within limit error = %v", err)
				}
			}
			if err := state.checkRate(tt.name, tt.limit); ErrorCode(err) != "rate_limited" {
				t.Fatalf("checkRate() over limit error = %v", err)
			}
			now = now.Add(tt.limit.Window)
			if err := state.checkRate(tt.name, tt.limit); err != nil {
				t.Fatalf("checkRate() after window error = %v", err)
			}
		})
	}
}

func TestAddGuardedToolRejectsInvalidPolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy GuardedToolPolicy[guardedTestInput]
	}{
		{name: "missing confirmation"},
		{name: "requests without window", policy: GuardedToolPolicy[guardedTestInput]{
			Confirmation: func(guardedTestInput) string { return "Confirm" },
			RateLimit:    RateLimit{Requests: 1},
		}},
		{name: "window without requests", policy: GuardedToolPolicy[guardedTestInput]{
			Confirmation: func(guardedTestInput) string { return "Confirm" },
			RateLimit:    RateLimit{Window: time.Minute},
		}},
		{name: "negative requests", policy: GuardedToolPolicy[guardedTestInput]{
			Confirmation: func(guardedTestInput) string { return "Confirm" },
			RateLimit:    RateLimit{Requests: -1, Window: time.Minute},
		}},
		{name: "negative window", policy: GuardedToolPolicy[guardedTestInput]{
			Confirmation: func(guardedTestInput) string { return "Confirm" },
			RateLimit:    RateLimit{Requests: 1, Window: -time.Minute},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AddGuardedTool(
				NewCatalog(), "", &mcp.Tool{Name: "test_tool"}, nil, tt.policy,
				func(context.Context, *mcp.CallToolRequest, mcpauth.Grant, guardedTestInput) (guardedTestOutput, error) {
					return guardedTestOutput{}, nil
				},
			)
			if err == nil {
				t.Fatal("AddGuardedTool() error = nil")
			}
		})
	}
}
