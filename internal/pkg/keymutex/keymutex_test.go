package keymutex

import (
	"context"
	"errors"
	"testing"
)

func TestLockContext(t *testing.T) {
	mutex := New()
	mutex.Lock("modem-1")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := mutex.LockContext(ctx, "modem-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("LockContext() error = %v, want %v", err, context.Canceled)
	}

	mutex.Unlock("modem-1")
	if err := mutex.LockContext(t.Context(), "modem-1"); err != nil {
		t.Fatalf("LockContext() after unlock error = %v", err)
	}
	mutex.Unlock("modem-1")
}
