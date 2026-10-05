package lpa

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/damonto/sigmo/internal/pkg/modem"
)

func TestPoolStopsWarmupBeforeSIMBecomesReady(t *testing.T) {
	tests := []struct {
		name string
		stop func(context.Context, *Pool, *modem.Modem) error
	}{
		{
			name: "generation retired",
			stop: func(ctx context.Context, p *Pool, m *modem.Modem) error {
				p.retire(ctx, m)
				return nil
			},
		},
		{
			name: "modem closed without retirement event",
			stop: func(_ context.Context, _ *Pool, m *modem.Modem) error {
				return m.Close()
			},
		},
		{
			name: "pool closed",
			stop: func(ctx context.Context, p *Pool, _ *modem.Modem) error {
				return p.Close(ctx)
			},
		},
		{
			name: "SIM changed during warmup",
			stop: func(ctx context.Context, p *Pool, m *modem.Modem) error {
				return p.invalidateSIMSlots(ctx, m, 1)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				p := &Pool{}
				m := &modem.Modem{EquipmentIdentifier: "imei-1"}
				p.warm(ctx, m)
				synctest.Wait()
				if err := tt.stop(ctx, p, m); err != nil {
					t.Error(err)
				}
				done := make(chan struct{})
				go func() {
					p.wg.Wait()
					close(done)
				}()
				synctest.Wait()
				select {
				case <-done:
				default:
					t.Error("warmup still runs after its owner stopped")
				}
				cancel()
				<-done
			})
		})
	}
}
