package server

import (
	"context"
	"log/slog"
	"time"
)

const updateHealthConfirmationDelay = 5 * time.Second

func confirmUpdateHealthy(ctx context.Context, markHealthy func() error) {
	confirmUpdateHealthyAfter(ctx, updateHealthConfirmationDelay, markHealthy)
}

func confirmUpdateHealthyAfter(ctx context.Context, delay time.Duration, markHealthy func() error) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	if err := markHealthy(); err != nil {
		slog.Warn("mark updated binary healthy", "error", err)
	}
}
