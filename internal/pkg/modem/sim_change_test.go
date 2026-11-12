package modem

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestSIMChangeWaitsForReplacementWithoutBlockingLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		device := qmiRegistryDevice("/dev/fake-control", "/sys/fake-modem")
		current := simRefreshTestModem(physicalDeviceKey(device), 1, true)
		current.deviceInfo = device
		replacement := simRefreshTestModem(current.Path(), 2, true)
		replacement.deviceInfo = device
		other := simRefreshTestModem("/sys/other-modem", 1, true)
		other.EquipmentIdentifier = "other-imei"
		r := &Registry{
			started: true, done: make(chan struct{}),
			modems: map[string]*Modem{current.Path(): current, other.Path(): other},
			open:   func(context.Context, wwanmodem.Device, uint64) (*Modem, error) { return replacement, nil },
		}
		defer r.Close()
		var removed, added bool
		r.subs = []subscription{{fn: func(event ModemEvent) error {
			// Internal subscribers must be able to inspect membership and retire
			// clients while public lookups are waiting for the SIM operation.
			present, err := r.Modems(t.Context())
			if err != nil {
				return err
			}
			switch event.Type {
			case ModemEventRemoved:
				removed = true
				if present[current.Path()] != nil {
					t.Error("retired modem remains in lifecycle inventory")
				}
			case ModemEventAdded:
				added = true
			}
			return nil
		}}}
		started, resume := make(chan struct{}), make(chan struct{})
		operation := make(chan error, 1)
		go func() {
			operation <- r.WithSIMChange(t.Context(), current, func(context.Context, *Modem) error {
				r.removeModem(current.Path(), current)
				close(started)
				<-resume
				r.applyDeviceEvent(t.Context(), wwanmodem.DeviceEvent{Type: wwanmodem.DevicePresent, Device: device})
				return nil
			})
		}()
		<-started
		found, listed, nextChange := make(chan *Modem, 1), make(chan map[string]*Modem, 1), make(chan *Modem, 1)
		go func() {
			modem, err := r.Find(t.Context(), current.EquipmentIdentifier)
			if err != nil {
				t.Error(err)
			}
			found <- modem
		}()
		go func() {
			modems, err := r.ReadyModems(t.Context())
			if err != nil {
				t.Error(err)
			}
			listed <- modems
		}()
		go func() {
			if err := r.WithSIMChange(t.Context(), current, func(_ context.Context, active *Modem) error {
				nextChange <- active
				return nil
			}); err != nil {
				t.Error(err)
			}
		}()
		synctest.Wait()
		if len(found) != 0 || len(listed) != 0 || len(nextChange) != 0 {
			t.Fatal("request escaped the pending SIM change")
		}
		if modem, err := r.Find(t.Context(), other.EquipmentIdentifier); err != nil || modem != other {
			t.Fatalf("unrelated modem lookup = %v, %v", modem, err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := r.Find(ctx, current.EquipmentIdentifier); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled lookup = %v", err)
		}
		if _, err := r.ReadyModems(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled inventory = %v", err)
		}
		close(resume)
		if err := <-operation; err != nil {
			t.Fatal(err)
		}
		if modem := <-found; modem != replacement {
			t.Fatalf("lookup returned generation %v", modem)
		}
		if modems := <-listed; len(modems) != 2 || modems[current.Path()] != replacement {
			t.Fatalf("inventory = %v, want both devices and the replacement", modems)
		}
		if modem := <-nextChange; modem != replacement {
			t.Fatal("queued SIM change received the retired modem")
		}
		synctest.Wait()
		if !removed || !added {
			t.Fatal("internal lifecycle events were suppressed")
		}
	})
}

func TestSIMChangeStopsOnRemovalShutdownAndTimeout(t *testing.T) {
	for _, reason := range []string{"removed", "shutdown", "timeout"} {
		t.Run(reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				device := qmiRegistryDevice("/dev/fake-control", "/sys/fake-modem")
				current := simRefreshTestModem(physicalDeviceKey(device), 1, true)
				current.deviceInfo = device
				r := &Registry{started: true, done: make(chan struct{}), modems: map[string]*Modem{current.Path(): current}}
				defer r.Close()
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				started := make(chan struct{})
				operation, lookup := make(chan error, 1), make(chan error, 1)
				go func() {
					operation <- r.WithSIMChange(ctx, current, func(ctx context.Context, _ *Modem) error {
						close(started)
						<-ctx.Done()
						return ctx.Err()
					})
				}()
				<-started
				go func() {
					_, err := r.Find(t.Context(), current.EquipmentIdentifier)
					lookup <- err
				}()
				synctest.Wait()
				var wantOperation, wantLookup error
				switch reason {
				case "removed":
					r.applyDeviceEvent(t.Context(), wwanmodem.DeviceEvent{Type: wwanmodem.DeviceRemoved, Device: device})
					wantOperation, wantLookup = ErrNotFound, ErrNotFound
				case "shutdown":
					if err := r.Close(); err != nil {
						t.Fatal(err)
					}
					wantOperation, wantLookup = errRegistryClosed, errRegistryClosed
				case "timeout":
					time.Sleep(time.Minute)
					wantOperation = context.DeadlineExceeded
				}
				if err := <-operation; !errors.Is(err, wantOperation) {
					t.Fatalf("operation = %v, want %v", err, wantOperation)
				}
				if err := <-lookup; !errors.Is(err, wantLookup) {
					t.Fatalf("lookup = %v, want %v", err, wantLookup)
				}
			})
		})
	}
}

func TestSIMRefreshRecoveryWaitsForNewGeneration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		device := qmiRegistryDevice("/dev/fake-control", "/sys/fake-modem")
		current := simRefreshTestModem(physicalDeviceKey(device), 1, true)
		current.deviceInfo = device
		replacement := simRefreshTestModem(current.Path(), 2, true)
		replacement.deviceInfo = device
		opening, resume := make(chan struct{}), make(chan struct{})
		r := &Registry{
			started: true, done: make(chan struct{}), modems: map[string]*Modem{current.Path(): current},
			discover: func(context.Context) ([]wwanmodem.Device, error) { return []wwanmodem.Device{device}, nil },
			open: func(context.Context, wwanmodem.Device, uint64) (*Modem, error) {
				close(opening)
				<-resume
				return replacement, nil
			},
		}
		defer r.Close()
		recovered := make(chan struct{})
		go func() {
			r.handleModemFailure(t.Context(), modemFailure{modem: current, err: errSIMRefreshReload})
			close(recovered)
		}()
		<-opening
		found := make(chan *Modem, 1)
		go func() {
			modem, err := r.Find(t.Context(), current.EquipmentIdentifier)
			if err != nil {
				t.Error(err)
			}
			found <- modem
		}()
		synctest.Wait()
		if len(found) != 0 {
			t.Fatal("lookup returned during modem recovery")
		}
		close(resume)
		<-recovered
		if modem := <-found; modem != replacement {
			t.Fatal("lookup did not return the recovered modem")
		}
	})
}

func TestSIMChangePreservesOperationErrorOnCancellation(t *testing.T) {
	current := simRefreshTestModem("/sys/fake-modem", 1, true)
	r := &Registry{started: true, modems: map[string]*Modem{current.Path(): current}}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	operationErr := errors.New("enable command lost its response")
	err := r.WithSIMChange(ctx, current, func(context.Context, *Modem) error {
		cancel()
		return operationErr
	})
	if !errors.Is(err, operationErr) || !errors.Is(err, context.Canceled) {
		t.Fatalf("SIM change error = %v, want operation error and cancellation", err)
	}
}
