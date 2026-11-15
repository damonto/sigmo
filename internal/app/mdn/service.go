// Package mdn fills missing active-line numbers from history and carrier entitlements.
package mdn

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/carrierconfig"
	"github.com/damonto/sigmo/internal/pkg/mdn"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modemtask"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

const (
	reconcileInterval = 30 * time.Second
	lookupTimeout     = 100 * time.Second
)

type numberStore interface {
	SIMProfileNumbers(context.Context, []string) (map[string]string, error)
}

// Config supplies the existing registry, ICCID history and live Pro authorization.
type Config struct {
	Registry      modemtask.Registry
	Store         numberStore
	CarrierConfig func(context.Context) (*carrierconfig.Config, error)
	Allowed       func() bool
}

// Service owns one cancellable lookup per modem generation.
type Service struct {
	config Config
	query  func(context.Context, *modem.Modem, modem.SIMIdentity, *mdn.Configs, func() error) (string, error)
}

// New creates a service without opening devices or starting background work.
func New(config Config) *Service { return &Service{config: config, query: queryNumber} }

// Run follows modem presence until cancellation, then joins all outstanding work.
func (s *Service) Run(ctx context.Context) error {
	if s.config.Store == nil || s.config.CarrierConfig == nil || s.config.Allowed == nil {
		return errors.New("mdn dependencies are required")
	}
	return modemtask.Run(ctx, s.config.Registry, s.runModem)
}

// lookup keeps cancellation, completion and the SIM target together. Its owner
// retires canceled work even if authorization is restored before it returns.
type lookup struct {
	target  modem.SIMIdentity
	cancel  context.CancelFunc
	retired bool
	done    chan struct{}

	// Written by the lookup goroutine and read only after done closes.
	number string
	err    error
}

func (l *lookup) retire() {
	l.retired = true
	l.cancel()
}

func (s *Service) startLookup(ctx context.Context, m *modem.Modem, target modem.SIMIdentity, catalog *mdn.Configs) *lookup {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	active := &lookup{target: target, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(active.done)
		defer cancel()
		check := func() error { return s.check(ctx, m, target) }
		active.number, active.err = s.query(ctx, m, target, catalog, check)
	}()
	return active
}

func (s *Service) runModem(ctx context.Context, m *modem.Modem) {
	wake := make(chan struct{}, 1)
	unsubscribe, err := s.config.Registry.Subscribe(ctx, func(event modem.ModemEvent) error {
		if event.Modem == m {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
		return nil
	})
	if err != nil {
		m.Logger().Warn("subscribe MDN changes", "error", err)
		// Polling still reconciles SIM state and authorization if events fail.
		unsubscribe = func() {}
	}
	defer unsubscribe()
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	var (
		target  modem.SIMIdentity
		catalog *mdn.Configs
		allowed bool
		retry   retryState
		active  *lookup
	)
	defer func() {
		if active != nil {
			active.retire()
			<-active.done
		}
	}()
	for {
		snapshot := m.Snapshot()
		permitted := s.config.Allowed()
		if target != snapshot.SIMIdentity || allowed != permitted {
			if active != nil {
				active.retire()
			}
			target, allowed = snapshot.SIMIdentity, permitted
			retry = retryState{}
		}
		ready := snapshot.Status.SIM == wwanmodem.SIMStateReady && !snapshot.AirplaneMode() && target.ICCID != ""
		if ready && snapshot.Number == "" {
			snapshot = s.restoreNumber(ctx, m, snapshot)
		}
		if active != nil && (!ready || snapshot.Number != "" || !allowed) {
			active.retire()
		}
		if ready && snapshot.Number == "" && allowed && active == nil {
			config, err := s.config.CarrierConfig(ctx)
			if err != nil {
				m.Logger().Debug("load MDN carrier configuration", "error", err)
			} else if config != nil {
				if !catalog.Equal(config.MDN) {
					retry = retryState{}
				}
				catalog = config.MDN
				if catalog != nil && retry.ready() {
					active = s.startLookup(ctx, m, target, catalog)
				}
			}
		}
		var done <-chan struct{}
		if active != nil {
			done = active.done
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-wake:
		case <-done:
			completed := active
			active = nil
			if completed.retired {
				continue
			}
			if completed.err == nil && s.check(ctx, m, completed.target) == nil {
				m.SetKnownNumber(completed.target, completed.number)
			}
			retry.record(completed.err)
			if completed.err != nil && !errors.Is(completed.err, context.Canceled) {
				m.Logger().Debug("query missing SIM number", "error", completed.err, "retry", !retry.stopped)
			}
		}
	}
}

func (s *Service) restoreNumber(ctx context.Context, m *modem.Modem, snapshot modem.ModemSnapshot) modem.ModemSnapshot {
	numbers, err := s.config.Store.SIMProfileNumbers(ctx, []string{snapshot.SIMIdentity.ICCID})
	if err != nil {
		m.Logger().Warn("restore SIM number", "error", err)
		return snapshot
	}
	// Previously confirmed numbers remain available after Pro expires.
	if number := strings.TrimSpace(numbers[snapshot.SIMIdentity.ICCID]); number != "" {
		m.SetKnownNumber(snapshot.SIMIdentity, number)
		return m.Snapshot()
	}
	return snapshot
}

func (s *Service) check(ctx context.Context, m *modem.Modem, target modem.SIMIdentity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot := m.Snapshot()
	if !s.config.Allowed() || snapshot.SIMIdentity != target || snapshot.Number != "" ||
		snapshot.Status.SIM != wwanmodem.SIMStateReady || snapshot.AirplaneMode() {
		return context.Canceled
	}
	select {
	case <-m.Done():
		return context.Canceled
	default:
		return nil
	}
}

type retryState struct {
	attempts int
	stopped  bool
	retryAt  time.Time
}

func (r retryState) ready() bool { return !r.stopped && !time.Now().Before(r.retryAt) }

func (r *retryState) record(err error) {
	if err == nil {
		r.stopped = true
		return
	}
	if errors.Is(err, context.Canceled) {
		// A card can change before firmware publishes its new identity.
		r.retryAt = time.Now().Add(reconcileInterval)
		return
	}
	r.attempts++
	r.stopped = !mdn.Temporary(err)
	r.retryAt = time.Now().Add(retryDelay(r.attempts, err))
}

func retryDelay(attempt int, err error) time.Duration {
	delays := [...]time.Duration{30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
	delay := delays[min(max(attempt-1, 0), len(delays)-1)]
	var status *mdn.StatusError
	if errors.As(err, &status) {
		delay = max(delay, status.RetryAfter)
	}
	return delay
}
