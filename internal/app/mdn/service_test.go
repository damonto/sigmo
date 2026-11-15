package mdn

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/damonto/sigmo/internal/pkg/carrierconfig"
	"github.com/damonto/sigmo/internal/pkg/mdn"
	"github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

type fakeRegistry struct {
	mu      sync.Mutex
	present map[string]*modem.Modem
	next    int
	subs    map[int]func(modem.ModemEvent) error
}

func (r *fakeRegistry) Modems(context.Context) (map[string]*modem.Modem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.present), nil
}
func (r *fakeRegistry) Subscribe(_ context.Context, fn func(modem.ModemEvent) error) (func(), error) {
	r.mu.Lock()
	if r.subs == nil {
		r.subs = make(map[int]func(modem.ModemEvent) error)
	}
	r.next++
	id := r.next
	r.subs[id] = fn
	r.mu.Unlock()
	return func() { r.mu.Lock(); delete(r.subs, id); r.mu.Unlock() }, nil
}
func (r *fakeRegistry) emit(e modem.ModemEvent) {
	r.mu.Lock()
	subs := slices.Collect(maps.Values(r.subs))
	r.mu.Unlock()
	for _, fn := range subs {
		_ = fn(e)
	}
}

type fakeStore map[string]string

func (s fakeStore) SIMProfileNumbers(context.Context, []string) (map[string]string, error) {
	return maps.Clone(s), nil
}
func readyModem() *modem.Modem {
	return &modem.Modem{EquipmentIdentifier: "123456789012345", SIM: &modem.SIM{Identifier: "sim-a"}, Status: wwanmodem.Status{SIM: wwanmodem.SIMStateReady, Power: wwanmodem.PowerStateOn}}
}
func testService(m *modem.Modem, allowed *atomic.Bool, store fakeStore) (*Service, *fakeRegistry) {
	registry := &fakeRegistry{present: map[string]*modem.Modem{"modem": m}}
	config := &carrierconfig.Config{MDN: new(mdn.Configs)}
	service := New(Config{Registry: registry, Store: store, Allowed: func() bool { return allowed.Load() }, CarrierConfig: func(context.Context) (*carrierconfig.Config, error) { return config, nil }})
	return service, registry
}

func testCatalog(t *testing.T, server string) *carrierconfig.Config {
	t.Helper()
	data, err := json.Marshal([]mdn.Profile{{MCC: "001", MNC: "01", Server: server,
		ProtocolVersion: "2", SigningVersion: 2, UserAgent: "test", CountryISO: "GB"}})
	if err != nil {
		t.Fatal(err)
	}
	profiles := new(mdn.Configs)
	if err := json.Unmarshal(data, profiles); err != nil {
		t.Fatal(err)
	}
	return &carrierconfig.Config{MDN: profiles}
}
func startService(t *testing.T, s *Service) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return cancel, done
}
func stopService(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	synctest.Wait()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestHistoryWithoutProAndRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		allowed := new(atomic.Bool)
		history := fakeStore{"sim-a": "+15551234567"}
		for range 2 {
			m := readyModem()
			s, _ := testService(m, allowed, history)
			s.config.CarrierConfig = func(context.Context) (*carrierconfig.Config, error) {
				t.Error("non-Pro fetched configuration")
				return nil, nil
			}
			s.query = func(context.Context, *modem.Modem, modem.SIMIdentity, *mdn.Configs, func() error) (string, error) {
				t.Error("history triggered a query")
				return "", nil
			}
			cancel, done := startService(t, s)
			synctest.Wait()
			if m.Snapshot().Number != history["sim-a"] {
				t.Fatal("history not restored")
			}
			m.SetIMSNumber(m.Snapshot().SIMIdentity, "+200")
			m.SetIMSNumber(m.Snapshot().SIMIdentity, "")
			if m.Snapshot().Number != history["sim-a"] {
				t.Fatal("IMS cleared restored history")
			}
			stopService(t, cancel, done)
		}
	})
}
func TestAuthorizationAndLatePublication(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		allowed := new(atomic.Bool)
		m := readyModem()
		s, r := testService(m, allowed, fakeStore{})
		var calls atomic.Int32
		release := make(chan struct{})
		canceled := make(chan struct{})
		s.query = func(ctx context.Context, _ *modem.Modem, _ modem.SIMIdentity, _ *mdn.Configs, check func() error) (string, error) {
			calls.Add(1)
			if err := check(); err != nil {
				return "", err
			}
			<-ctx.Done()
			close(canceled)
			<-release
			return "+15551234567", nil
		}
		cancel, done := startService(t, s)
		synctest.Wait()
		if calls.Load() != 0 {
			t.Fatal("query without Pro")
		}
		allowed.Store(true)
		r.emit(modem.ModemEvent{Type: modem.ModemEventNumberChanged, Modem: m})
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatal("Pro activation did not start lookup")
		}
		allowed.Store(false)
		time.Sleep(30 * time.Second)
		synctest.Wait()
		<-canceled
		close(release)
		synctest.Wait()
		if m.Snapshot().Number != "" {
			t.Fatal("published after Pro expired")
		}
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatal("queried after Pro expired")
		}
		stopService(t, cancel, done)
	})
}
func TestConcurrentIMSAndGenerationChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		allowed := new(atomic.Bool)
		allowed.Store(true)
		m := readyModem()
		s, r := testService(m, allowed, fakeStore{})
		release := make(chan struct{})
		var calls atomic.Int32
		s.query = func(ctx context.Context, current *modem.Modem, _ modem.SIMIdentity, _ *mdn.Configs, _ func() error) (string, error) {
			calls.Add(1)
			if current == m {
				<-release
				return "+100", nil
			}
			<-ctx.Done()
			return "", ctx.Err()
		}
		cancel, done := startService(t, s)
		synctest.Wait()
		m.SetIMSNumber(m.Snapshot().SIMIdentity, "+200")
		r.emit(modem.ModemEvent{Type: modem.ModemEventNumberChanged, Modem: m})
		synctest.Wait()
		close(release)
		synctest.Wait()
		m.SetIMSNumber(m.Snapshot().SIMIdentity, "")
		if m.Snapshot().Number != "" {
			t.Fatal("MDN was published behind an IMS number")
		}
		replacement := readyModem()
		r.mu.Lock()
		r.present["modem"] = replacement
		r.mu.Unlock()
		r.emit(modem.ModemEvent{Type: modem.ModemEventChanged, Modem: replacement, Previous: m, Path: "modem", PreviousPath: "modem", Generation: 2})
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatalf("generation replacement: %d queries", calls.Load())
		}
		stopService(t, cancel, done)
	})
}
func TestRetryAndCatalogReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		allowed := new(atomic.Bool)
		allowed.Store(true)
		m := readyModem()
		s, _ := testService(m, allowed, fakeStore{})
		var calls atomic.Int32
		var catalog atomic.Pointer[carrierconfig.Config]
		catalog.Store(testCatalog(t, "https://carrier.example/mdn"))
		s.config.CarrierConfig = func(context.Context) (*carrierconfig.Config, error) { return catalog.Load(), nil }
		s.query = func(context.Context, *modem.Modem, modem.SIMIdentity, *mdn.Configs, func() error) (string, error) {
			calls.Add(1)
			if calls.Load() == 1 {
				return "", &mdn.StatusError{HTTP: true, Code: 429, RetryAfter: 2 * time.Minute}
			}
			return "", mdn.ErrUnsupported
		}
		cancel, done := startService(t, s)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatal("lookup not started")
		}
		// A refreshed license grant may decode the same catalog into a new object.
		catalog.Store(testCatalog(t, "https://carrier.example/mdn"))
		time.Sleep(90 * time.Second)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatal("ignored Retry-After")
		}
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatal("temporary failure not retried")
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatal("permanent failure retried")
		}
		catalog.Store(testCatalog(t, "https://carrier.example/mdn"))
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatal("identical catalog refresh reset a permanent failure")
		}
		catalog.Store(testCatalog(t, "https://replacement.example/mdn"))
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if calls.Load() != 3 {
			t.Fatal("catalog replacement did not retry")
		}
		stopService(t, cancel, done)
	})
}

func TestRenewedAuthorizationDiscardsCanceledLookup(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "late success"
		if failure {
			name = "late failure"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				allowed := new(atomic.Bool)
				allowed.Store(true)
				m := readyModem()
				s, registry := testService(m, allowed, fakeStore{})
				var calls atomic.Int32
				canceled := make(chan struct{})
				release := make(chan struct{})
				s.query = func(ctx context.Context, _ *modem.Modem, _ modem.SIMIdentity, _ *mdn.Configs, _ func() error) (string, error) {
					if calls.Add(1) != 1 {
						return "+200", nil
					}
					<-ctx.Done()
					close(canceled)
					<-release
					if failure {
						return "", mdn.ErrUnsupported
					}
					return "+100", nil
				}
				cancel, done := startService(t, s)
				synctest.Wait()
				allowed.Store(false)
				registry.emit(modem.ModemEvent{Type: modem.ModemEventNumberChanged, Modem: m})
				synctest.Wait()
				<-canceled
				allowed.Store(true)
				registry.emit(modem.ModemEvent{Type: modem.ModemEventNumberChanged, Modem: m})
				synctest.Wait()
				close(release)
				synctest.Wait()
				stopService(t, cancel, done)
				if m.Snapshot().Number != "+200" || calls.Load() != 2 {
					t.Fatalf("canceled lookup affected renewed authorization: number=%q queries=%d", m.Snapshot().Number, calls.Load())
				}
			})
		})
	}
}
func TestShutdownJoinsQuery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		allowed := new(atomic.Bool)
		allowed.Store(true)
		m := readyModem()
		s, _ := testService(m, allowed, fakeStore{})
		release := make(chan struct{})
		canceled := make(chan struct{})
		s.query = func(ctx context.Context, _ *modem.Modem, _ modem.SIMIdentity, _ *mdn.Configs, _ func() error) (string, error) {
			<-ctx.Done()
			close(canceled)
			<-release
			return "", ctx.Err()
		}
		cancel, done := startService(t, s)
		synctest.Wait()
		cancel()
		synctest.Wait()
		<-canceled
		select {
		case <-done:
			t.Fatal("returned before lookup exited")
		default:
		}
		close(release)
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}
func TestGuardBeforeHTTP(t *testing.T) {
	called := false
	transport := checkedTransport{check: func() error { return context.Canceled }, base: roundTripFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, nil })}
	req, _ := http.NewRequest("POST", "https://carrier.example", nil)
	if _, err := transport.RoundTrip(req); !errors.Is(err, context.Canceled) || called {
		t.Fatal("HTTP guard did not stop request")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLookupPublishesAndKeepsNumber(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		allowed := new(atomic.Bool)
		allowed.Store(true)
		m := readyModem()
		s, r := testService(m, allowed, fakeStore{})
		var calls atomic.Int32
		s.query = func(context.Context, *modem.Modem, modem.SIMIdentity, *mdn.Configs, func() error) (string, error) {
			calls.Add(1)
			return "+15551234567", nil
		}
		cancel, done := startService(t, s)
		synctest.Wait()
		if m.Snapshot().Number != "+15551234567" {
			t.Fatal("successful lookup not published")
		}
		allowed.Store(false)
		r.emit(modem.ModemEvent{Type: modem.ModemEventNumberChanged, Modem: m})
		synctest.Wait()
		if m.Snapshot().Number != "+15551234567" || calls.Load() != 1 {
			t.Fatal("Pro expiry erased or requeried a known number")
		}
		stopService(t, cancel, done)
	})
}

func TestSIMActivationCancelsAndDiscardsOldLookup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		allowed := new(atomic.Bool)
		allowed.Store(true)
		m := readyModem()
		s, r := testService(m, allowed, fakeStore{})
		var calls atomic.Int32
		releaseOld := make(chan struct{})
		canceled := make(chan struct{})
		s.query = func(ctx context.Context, _ *modem.Modem, _ modem.SIMIdentity, _ *mdn.Configs, _ func() error) (string, error) {
			if calls.Add(1) == 1 {
				<-ctx.Done()
				close(canceled)
				<-releaseOld
				return "+100", nil
			}
			return "+200", nil
		}
		cancel, done := startService(t, s)
		synctest.Wait()
		release, err := m.ReserveSIMSlot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		m.InvalidateSIMActivation()
		release()
		r.emit(modem.ModemEvent{Type: modem.ModemEventSIMChanged, Modem: m})
		synctest.Wait()
		<-canceled
		if calls.Load() != 1 {
			t.Fatal("started another lookup before the old one exited")
		}
		close(releaseOld)
		synctest.Wait()
		if m.Snapshot().Number != "+200" || calls.Load() != 2 {
			t.Fatal("stale SIM activation published its number or blocked reactivation")
		}
		stopService(t, cancel, done)
	})
}
