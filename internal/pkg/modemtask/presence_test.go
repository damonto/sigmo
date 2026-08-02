package modemtask

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/modem"
)

func TestRunStaleWorkerDoesNotReleaseReplacement(t *testing.T) {
	const path = "/sys/devices/modem-1"
	old := &modem.Modem{EquipmentIdentifier: "imei-1"}
	replacement := &modem.Modem{EquipmentIdentifier: "imei-1"}
	registry := newFakeRegistry(map[string]*modem.Modem{path: old})
	started := make(chan *modem.Modem, 2)
	oldCanceled := make(chan struct{})
	oldReturned := make(chan struct{})
	newCanceled := make(chan struct{})
	releaseOld := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, registry, func(workerCtx context.Context, m *modem.Modem) {
			started <- m
			<-workerCtx.Done()
			switch m {
			case old:
				close(oldCanceled)
				<-releaseOld
				close(oldReturned)
			case replacement:
				close(newCanceled)
			}
		})
	}()
	defer func() {
		cancel()
		select {
		case <-releaseOld:
		default:
			close(releaseOld)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Run() did not stop")
		}
	}()

	if got := receiveModem(t, started); got != old {
		t.Fatalf("initial modem = %p, want %p", got, old)
	}
	registry.publish(modem.ModemEvent{Type: modem.ModemEventChanged, Modem: replacement, Previous: old, Path: path, PreviousPath: path, Generation: 2})
	if got := receiveModem(t, started); got != replacement {
		t.Fatalf("replacement modem = %p, want %p", got, replacement)
	}
	waitSignal(t, oldCanceled, "old worker cancellation")
	close(releaseOld)
	waitSignal(t, oldReturned, "old worker return")

	registry.publish(modem.ModemEvent{Type: modem.ModemEventRemoved, Modem: replacement, Path: path, Generation: 2})
	waitSignal(t, newCanceled, "replacement worker cancellation")
}

func TestRunWaitsForWorkersToExit(t *testing.T) {
	const path = "/sys/devices/modem-1"
	registry := newFakeRegistry(map[string]*modem.Modem{path: {EquipmentIdentifier: "imei-1"}})
	started := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, registry, func(workerCtx context.Context, _ *modem.Modem) {
			close(started)
			<-workerCtx.Done()
			close(canceled)
			<-release
		})
	}()

	waitSignal(t, started, "worker start")
	cancel()
	waitSignal(t, canceled, "worker cancellation")
	select {
	case err := <-done:
		t.Fatalf("Run() returned before worker exit: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after worker exit")
	}
}

type fakeRegistry struct {
	mu          sync.Mutex
	modems      map[string]*modem.Modem
	subscribers []func(modem.ModemEvent) error
}

func newFakeRegistry(modems map[string]*modem.Modem) *fakeRegistry {
	return &fakeRegistry{modems: modems}
}

func (r *fakeRegistry) Modems(context.Context) (map[string]*modem.Modem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[string]*modem.Modem, len(r.modems))
	for key, m := range r.modems {
		result[key] = m
	}
	return result, nil
}

func (r *fakeRegistry) Subscribe(fn func(modem.ModemEvent) error) (func(), error) {
	r.mu.Lock()
	r.subscribers = append(r.subscribers, fn)
	index := len(r.subscribers) - 1
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		if index < len(r.subscribers) {
			r.subscribers[index] = nil
		}
		r.mu.Unlock()
	}, nil
}

func (r *fakeRegistry) publish(event modem.ModemEvent) {
	r.mu.Lock()
	subscribers := append([]func(modem.ModemEvent) error(nil), r.subscribers...)
	r.mu.Unlock()
	for _, subscriber := range subscribers {
		if subscriber != nil {
			_ = subscriber(event)
		}
	}
}

func receiveModem(t *testing.T, values <-chan *modem.Modem) *modem.Modem {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for modem worker")
		return nil
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
