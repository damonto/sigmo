package nativepush

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"
)

func (d *Dispatcher) runCalls(ctx context.Context) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := d.drainCalls(ctx); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "dispatch call invitations", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) drainCalls(ctx context.Context) error {
	items, err := d.store.NativeCallDeliveries(ctx, time.Now())
	if err != nil {
		return err
	}
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for _, item := range items {
		group.Go(func() error {
			attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			err := d.deliver(attempt, item)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				retry, retryAt := deliveryRetry(err)
				slog.WarnContext(ctx, "deliver call invitation", "error", err, "retry", retry)
				if retry {
					if minimum := time.Now().Add(time.Second); retryAt.Before(minimum) {
						retryAt = minimum
					}
					return d.store.FinishNativeCall(ctx, item, retryAt)
				}
			}
			return d.store.FinishNativeCall(ctx, item, time.Time{})
		})
	}
	return group.Wait()
}
