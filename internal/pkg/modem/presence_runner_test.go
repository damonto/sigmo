package modem

import (
	"context"
	"testing"
	"time"
)

func TestPresenceTaskStaleWorkerDoesNotReleaseReplacement(t *testing.T) {
	const path = "/sys/devices/modem-1"
	old := &Modem{deviceKey: path, generation: 1, EquipmentIdentifier: "imei-1"}
	registry := &Registry{modems: map[string]*Modem{path: old}, started: true}
	started := make(chan uint64, 2)
	oldCanceled := make(chan struct{})
	oldReturned := make(chan struct{})
	newCanceled := make(chan struct{})
	releaseOld := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunPresenceTask(ctx, registry, func(workerCtx context.Context, modem *Modem) {
			started <- modem.Generation()
			<-workerCtx.Done()
			switch modem.Generation() {
			case 1:
				close(oldCanceled)
				<-releaseOld
				close(oldReturned)
			case 2:
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
				t.Errorf("RunPresenceTask() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("RunPresenceTask() did not stop")
		}
	}()

	if got := receiveGeneration(t, started); got != 1 {
		t.Fatalf("initial generation = %d, want 1", got)
	}
	replacement := &Modem{deviceKey: path, generation: 2, EquipmentIdentifier: "imei-1"}
	publishRegistryEvent(registry, ModemEvent{Type: ModemEventChanged, Modem: replacement, Previous: old, Path: path, Generation: 2})
	if got := receiveGeneration(t, started); got != 2 {
		t.Fatalf("replacement generation = %d, want 2", got)
	}
	waitSignal(t, oldCanceled, "old worker cancellation")
	close(releaseOld)
	waitSignal(t, oldReturned, "old worker return")

	publishRegistryEvent(registry, ModemEvent{Type: ModemEventRemoved, Modem: replacement, Path: path, Generation: 2})
	waitSignal(t, newCanceled, "replacement worker cancellation")
}

func TestPresenceTaskWaitsForWorkersToExit(t *testing.T) {
	const path = "/sys/devices/modem-1"
	registry := &Registry{
		modems:  map[string]*Modem{path: {deviceKey: path, generation: 1, EquipmentIdentifier: "imei-1"}},
		started: true,
	}
	started := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunPresenceTask(ctx, registry, func(workerCtx context.Context, _ *Modem) {
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
		t.Fatalf("RunPresenceTask() returned before worker exit: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunPresenceTask() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunPresenceTask() did not return after worker exit")
	}
}

func publishRegistryEvent(registry *Registry, event ModemEvent) {
	registry.mu.RLock()
	subscribers := append([]subscription(nil), registry.subs...)
	registry.mu.RUnlock()
	registry.publish(subscribers, event)
}

func receiveGeneration(t *testing.T, values <-chan uint64) uint64 {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker generation")
		return 0
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
