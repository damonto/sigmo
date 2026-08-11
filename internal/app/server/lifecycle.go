package server

import (
	"context"
	"errors"
	"fmt"
)

type cleanupStack struct {
	cleanups []Cleanup
}

func (s *cleanupStack) Add(cleanup Cleanup) {
	if cleanup == nil {
		return
	}
	s.cleanups = append(s.cleanups, cleanup)
}

func (s *cleanupStack) Close(ctx context.Context) error {
	var result error
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		result = errors.Join(result, s.cleanups[i](ctx))
	}
	s.cleanups = nil
	return result
}

func superviseRunner(ctx context.Context, name string, runner Runner) error {
	if runner == nil {
		return fmt.Errorf("%s runner is nil", name)
	}
	err := runner(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && onlyContextCancellation(err, ctxErr) {
			return nil
		}
		return fmt.Errorf("%s runner: %w", name, err)
	}
	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("%s runner stopped unexpectedly", name)
}

func onlyContextCancellation(err, ctxErr error) bool {
	if err == nil || ctxErr == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		if len(joined.Unwrap()) == 0 {
			return false
		}
		for _, child := range joined.Unwrap() {
			if !onlyContextCancellation(child, ctxErr) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyContextCancellation(wrapped.Unwrap(), ctxErr)
	}
	return errors.Is(err, ctxErr)
}

func finalizeRunError(runErr, shutdownCause error) error {
	gracefulShutdown := errors.Is(shutdownCause, errSystemSignal) || errors.Is(shutdownCause, ErrRestart)
	if gracefulShutdown && onlyContextCancellation(runErr, context.Canceled) {
		runErr = nil
	}
	if runErr == nil && errors.Is(shutdownCause, ErrRestart) {
		return ErrRestart
	}
	return runErr
}
