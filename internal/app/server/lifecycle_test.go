package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestCleanupStackClosesInReverseAndJoinsErrors(t *testing.T) {
	firstErr := errors.New("first cleanup")
	lastErr := errors.New("last cleanup")
	var order []string
	var cleanups cleanupStack
	cleanups.Add(func(context.Context) error {
		order = append(order, "first")
		return firstErr
	})
	cleanups.Add(func(context.Context) error {
		order = append(order, "middle")
		return nil
	})
	cleanups.Add(func(context.Context) error {
		order = append(order, "last")
		return lastErr
	})

	err := cleanups.Close(t.Context())
	if !slices.Equal(order, []string{"last", "middle", "first"}) {
		t.Fatalf("cleanup order = %v, want reverse registration order", order)
	}
	if !errors.Is(err, firstErr) || !errors.Is(err, lastErr) {
		t.Fatalf("Close() error = %v, want both cleanup errors", err)
	}
	if err := cleanups.Close(t.Context()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestRuntimeClosesExtensionResourcesInReverse(t *testing.T) {
	var order []int
	runtime := &Runtime{}
	runtime.AddCleanup(
		func(context.Context) error {
			order = append(order, 1)
			return nil
		},
		func(context.Context) error {
			order = append(order, 2)
			return nil
		},
	)

	if err := runtime.close(t.Context()); err != nil {
		t.Fatalf("close() error = %v", err)
	}
	if !slices.Equal(order, []int{2, 1}) {
		t.Fatalf("cleanup order = %v, want [2 1]", order)
	}
}

func TestSuperviseRunner(t *testing.T) {
	runnerErr := errors.New("runner stopped")
	tests := []struct {
		name        string
		cancel      bool
		runner      Runner
		wantErr     error
		wantMessage string
	}{
		{
			name:    "returns error",
			runner:  func(context.Context) error { return runnerErr },
			wantErr: runnerErr,
		},
		{
			name:        "returns nil",
			runner:      func(context.Context) error { return nil },
			wantMessage: "stopped unexpectedly",
		},
		{
			name:   "returns cancellation after cancellation",
			cancel: true,
			runner: func(ctx context.Context) error { return ctx.Err() },
		},
		{
			name:    "returns cleanup error after cancellation",
			cancel:  true,
			runner:  func(ctx context.Context) error { return errors.Join(ctx.Err(), runnerErr) },
			wantErr: runnerErr,
		},
		{
			name:        "nil runner",
			wantMessage: "runner is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			if tt.cancel {
				cancel()
			} else {
				defer cancel()
			}
			err := superviseRunner(ctx, "test", tt.runner)
			if tt.cancel && tt.wantErr == nil {
				if err != nil {
					t.Fatalf("superviseRunner() error = %v, want nil", err)
				}
				return
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("superviseRunner() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantMessage != "" && (err == nil || !strings.Contains(err.Error(), tt.wantMessage)) {
				t.Fatalf("superviseRunner() error = %v, want message %q", err, tt.wantMessage)
			}
		})
	}
}

func TestFinalizeRunError(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	runnerErr := errors.New("runner failed")
	startupErr := errors.New("start registry: context canceled")
	tests := []struct {
		name          string
		runErr        error
		shutdownCause error
		wantErr       error
	}{
		{
			name:          "signal during startup",
			runErr:        startupErr,
			shutdownCause: errSystemSignal,
			wantErr:       startupErr,
		},
		{
			name:          "wrapped signal during startup",
			runErr:        fmt.Errorf("start registry: %w", context.Canceled),
			shutdownCause: errSystemSignal,
		},
		{
			name:          "signal with cleanup failure",
			runErr:        errors.Join(context.Canceled, cleanupErr),
			shutdownCause: errSystemSignal,
			wantErr:       cleanupErr,
		},
		{
			name:          "runner failure",
			runErr:        runnerErr,
			shutdownCause: errSystemSignal,
			wantErr:       runnerErr,
		},
		{
			name:          "unexpected runner cancellation without signal",
			runErr:        fmt.Errorf("extension runner: %w", context.Canceled),
			shutdownCause: fmt.Errorf("extension runner: %w", context.Canceled),
			wantErr:       context.Canceled,
		},
		{
			name:          "restart",
			shutdownCause: ErrRestart,
			wantErr:       ErrRestart,
		},
		{
			name:          "restart during startup",
			runErr:        fmt.Errorf("start registry: %w", context.Canceled),
			shutdownCause: ErrRestart,
			wantErr:       ErrRestart,
		},
		{
			name:          "restart with cleanup failure",
			runErr:        cleanupErr,
			shutdownCause: ErrRestart,
			wantErr:       cleanupErr,
		},
		{
			name:          "signal wins over later restart",
			shutdownCause: errSystemSignal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := finalizeRunError(tt.runErr, tt.shutdownCause)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("finalizeRunError() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("finalizeRunError() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
