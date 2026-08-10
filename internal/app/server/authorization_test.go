package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfirmUpdateHealthy(t *testing.T) {
	tests := []struct {
		name   string
		cancel bool
		want   int32
	}{
		{name: "stable process", want: 1},
		{name: "stopped process", cancel: true, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			if tt.cancel {
				cancel()
			} else {
				defer cancel()
			}
			var calls atomic.Int32
			confirmUpdateHealthyAfter(ctx, time.Millisecond, func() error {
				calls.Add(1)
				return nil
			})
			if got := calls.Load(); got != tt.want {
				t.Fatalf("mark healthy calls = %d, want %d", got, tt.want)
			}
		})
	}
}
