package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const idempotencyRetention = 30 * time.Minute

type RateLimit struct {
	Requests int
	Window   time.Duration
}

type GuardedToolPolicy[In any] struct {
	Validate       func(context.Context, In) error
	Confirmation   func(In) string
	RateLimit      RateLimit
	IdempotencyKey func(In) string
}

type executionPolicyState struct {
	mu          sync.Mutex
	rateEvents  map[string][]time.Time
	idempotency map[string]*idempotencyEntry
	now         func() time.Time
	confirm     func(context.Context, *mcp.CallToolRequest, string) error
}

type idempotencyEntry struct {
	fingerprint string
	done        chan struct{}
	output      any
	err         error
	completedAt time.Time
	aborted     bool
}

type idempotencyClaim struct {
	entry    *idempotencyEntry
	cached   bool
	complete func(any, error)
	abort    func()
}

func executeGuardedTool[In, Out any](ctx context.Context, req *mcp.CallToolRequest, exec *execution, toolName string, policy GuardedToolPolicy[In], handler ToolHandler[In, Out], input In) (out Out, err error) {
	claim, err := claimIdempotency(ctx, &exec.policy, toolName, policy.IdempotencyKey, input)
	if err != nil {
		return out, err
	}
	if claim.cached {
		if claim.entry.err != nil {
			return out, claim.entry.err
		}
		cached, ok := claim.entry.output.(Out)
		if !ok {
			return out, NewToolError("operation_failed", "the cached tool result has an unexpected type", nil)
		}
		return cached, nil
	}
	completed := false
	defer func() {
		if recovered := recover(); recovered != nil {
			if claim.abort != nil {
				claim.abort()
			}
			panic(recovered)
		}
		if !completed && claim.abort != nil {
			claim.abort()
		}
	}()
	if policy.Validate != nil {
		if err := policy.Validate(ctx, input); err != nil {
			return out, err
		}
	}
	if err := exec.policy.checkRate(toolName, policy.RateLimit); err != nil {
		return out, err
	}
	message := strings.TrimSpace(policy.Confirmation(input))
	if message == "" {
		return out, errors.New("guarded MCP tool confirmation message is required")
	}
	if err := exec.policy.requireConfirmation(ctx, req, message); err != nil {
		return out, err
	}
	out, err = handler(ctx, req, exec.grant, input)
	if claim.complete != nil {
		claim.complete(out, err)
	}
	completed = true
	return out, err
}

func (s *executionPolicyState) checkRate(tool string, limit RateLimit) error {
	if limit.Requests == 0 && limit.Window == 0 {
		return nil
	}
	if limit.Requests < 1 || limit.Window <= 0 {
		return errors.New("invalid MCP tool rate limit")
	}
	now := s.timeNow()
	cutoff := now.Add(-limit.Window)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rateEvents == nil {
		s.rateEvents = make(map[string][]time.Time)
	}
	events := s.rateEvents[tool]
	first := 0
	for first < len(events) && !events[first].After(cutoff) {
		first++
	}
	events = events[first:]
	if len(events) >= limit.Requests {
		s.rateEvents[tool] = events
		return NewToolError("rate_limited", fmt.Sprintf("this API key may call %s at most %d times per %s", tool, limit.Requests, limit.Window), nil)
	}
	s.rateEvents[tool] = append(events, now)
	return nil
}

func claimIdempotency[In any](ctx context.Context, state *executionPolicyState, tool string, key func(In) string, input In) (idempotencyClaim, error) {
	if key == nil {
		return idempotencyClaim{}, nil
	}
	rawKey := strings.TrimSpace(key(input))
	if rawKey == "" {
		return idempotencyClaim{}, NewToolError("invalid_request", "idempotencyKey is required", nil)
	}
	if len(rawKey) > 128 {
		return idempotencyClaim{}, NewToolError("invalid_request", "idempotencyKey must not exceed 128 characters", nil)
	}
	data, err := json.Marshal(input)
	if err != nil {
		return idempotencyClaim{}, NewToolError("invalid_request", "the tool input cannot be fingerprinted", err)
	}
	fingerprint := sha256.Sum256(data)
	keyHash := sha256.Sum256([]byte(rawKey))
	cacheKey := tool + ":" + hex.EncodeToString(keyHash[:])
	for {
		now := state.timeNow()
		state.mu.Lock()
		if state.idempotency == nil {
			state.idempotency = make(map[string]*idempotencyEntry)
		}
		for currentKey, entry := range state.idempotency {
			if !entry.completedAt.IsZero() && !now.Before(entry.completedAt.Add(idempotencyRetention)) {
				delete(state.idempotency, currentKey)
			}
		}
		if entry := state.idempotency[cacheKey]; entry != nil {
			if entry.fingerprint != hex.EncodeToString(fingerprint[:]) {
				state.mu.Unlock()
				return idempotencyClaim{}, NewToolError("idempotency_conflict", "idempotencyKey was already used with different input", nil)
			}
			done := entry.done
			state.mu.Unlock()
			select {
			case <-ctx.Done():
				return idempotencyClaim{}, ctx.Err()
			case <-done:
				if entry.aborted {
					continue
				}
				return idempotencyClaim{entry: entry, cached: true}, nil
			}
		}
		entry := &idempotencyEntry{fingerprint: hex.EncodeToString(fingerprint[:]), done: make(chan struct{})}
		state.idempotency[cacheKey] = entry
		state.mu.Unlock()

		return idempotencyClaim{
			entry: entry,
			complete: func(output any, callErr error) {
				state.mu.Lock()
				entry.output = output
				entry.err = callErr
				entry.completedAt = state.timeNow()
				close(entry.done)
				state.mu.Unlock()
			},
			abort: func() {
				state.mu.Lock()
				if state.idempotency[cacheKey] == entry {
					delete(state.idempotency, cacheKey)
				}
				entry.aborted = true
				close(entry.done)
				state.mu.Unlock()
			},
		}, nil
	}
}

func (s *executionPolicyState) timeNow() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *executionPolicyState) requireConfirmation(ctx context.Context, req *mcp.CallToolRequest, message string) error {
	if s.confirm != nil {
		return s.confirm(ctx, req, message)
	}
	return RequireConfirmation(ctx, req, message)
}

func validateGuardedPolicy[In any](policy GuardedToolPolicy[In]) error {
	if policy.Confirmation == nil {
		return errors.New("guarded MCP tool confirmation is required")
	}
	if (policy.RateLimit.Requests == 0) != (policy.RateLimit.Window == 0) {
		return errors.New("guarded MCP tool rate limit requires requests and window")
	}
	if policy.RateLimit.Requests < 0 || policy.RateLimit.Window < 0 {
		return errors.New("guarded MCP tool rate limit must be positive")
	}
	return nil
}
