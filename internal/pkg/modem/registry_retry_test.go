package modem

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"
)

func TestRegistryRetriesOpenWithHealthyWatcher(t *testing.T) {
	tests := []struct {
		name              string
		startup           bool
		removed           bool
		missedRemoval     bool
		discoverFailsOnce bool
		cidExhausted      bool
		wantOpens         int
		wantModem         bool
	}{
		{name: "startup", startup: true, wantOpens: 2, wantModem: true},
		{name: "hotplug", wantOpens: 2, wantModem: true},
		{name: "discovery retry", discoverFailsOnce: true, wantOpens: 2, wantModem: true},
		{name: "removed before retry", removed: true, wantOpens: 1},
		{name: "missed removal", missedRemoval: true, wantOpens: 1},
		{name: "CID recovery remains bounded", cidExhausted: true, wantOpens: 2},
	}
	for _, protocol := range []struct {
		name     string
		portType wwanmodem.PortType
	}{
		{name: "QMI", portType: wwanmodem.PortQMI},
		{name: "MBIM", portType: wwanmodem.PortMBIM},
	} {
		for _, tt := range tests {
			if tt.cidExhausted && protocol.portType != wwanmodem.PortQMI {
				continue
			}
			t.Run(protocol.name+"/"+tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					r, err := NewRegistry()
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := r.Close(); err != nil {
							t.Error(err)
						}
					}()
					device := qmiRegistryDevice("/dev/fake-control", "/sys/fake-modem")
					device.Ports[0].Type = protocol.portType
					var present, discoverFails atomic.Bool
					present.Store(tt.startup)
					r.discover = func(context.Context) ([]wwanmodem.Device, error) {
						if discoverFails.Swap(false) {
							return nil, errors.New("device scan temporarily unavailable")
						}
						if present.Load() {
							return []wwanmodem.Device{device}, nil
						}
						return nil, nil
					}
					stream := make(chan wwanmodem.Result[wwanmodem.DeviceEvent])
					r.watchDevices = func(context.Context) (<-chan wwanmodem.Result[wwanmodem.DeviceEvent], error) {
						return stream, nil
					}
					opens := 0
					r.open = func(_ context.Context, d wwanmodem.Device, generation uint64) (*Modem, error) {
						opens++
						if tt.cidExhausted {
							return nil, qcom.QMIErrorClientIDsExhausted
						}
						if opens == 1 {
							return nil, context.DeadlineExceeded
						}
						return &Modem{deviceInfo: d, deviceKey: physicalDeviceKey(d), generation: generation, EquipmentIdentifier: "fake-imei"}, nil
					}
					if err := r.Start(t.Context()); err != nil {
						t.Fatal(err)
					}
					if !tt.startup {
						present.Store(true)
						stream <- wwanmodem.Result[wwanmodem.DeviceEvent]{Value: wwanmodem.DeviceEvent{Type: wwanmodem.DeviceAdded, Device: device}}
					}
					synctest.Wait()
					discoverFails.Store(tt.discoverFailsOnce)
					if tt.removed {
						// Keep discovery stale to verify the explicit removal cancels
						// the queued retry without waiting for another scan.
						stream <- wwanmodem.Result[wwanmodem.DeviceEvent]{Value: wwanmodem.DeviceEvent{Type: wwanmodem.DeviceRemoved, Device: device}}
					}
					if tt.missedRemoval {
						present.Store(false)
					}
					time.Sleep(5 * registryWatchRetryDelay)
					synctest.Wait()
					if opens != tt.wantOpens {
						t.Errorf("open attempts = %d, want %d", opens, tt.wantOpens)
					}
					_, err = r.Find(t.Context(), "fake-imei")
					if tt.wantModem && err != nil {
						t.Errorf("Find() error = %v, want recovered modem", err)
					}
					if !tt.wantModem && !errors.Is(err, ErrNotFound) {
						t.Errorf("Find() error = %v, want ErrNotFound", err)
					}
					if r.hasPendingOpens() {
						t.Error("completed or canceled retry remains pending")
					}
				})
			})
		}
	}
}

func TestRegistryCloseCancelsRetryOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := NewRegistry()
		if err != nil {
			t.Fatal(err)
		}
		device := qmiRegistryDevice("/dev/fake-control", "/sys/fake-modem")
		r.discover = func(context.Context) ([]wwanmodem.Device, error) { return []wwanmodem.Device{device}, nil }
		r.watchDevices = func(context.Context) (<-chan wwanmodem.Result[wwanmodem.DeviceEvent], error) {
			return make(chan wwanmodem.Result[wwanmodem.DeviceEvent]), nil
		}
		opens := 0
		started := make(chan struct{})
		stopped := make(chan struct{})
		r.open = func(ctx context.Context, _ wwanmodem.Device, _ uint64) (*Modem, error) {
			opens++
			if opens == 1 {
				return nil, context.DeadlineExceeded
			}
			close(started)
			<-ctx.Done()
			close(stopped)
			return nil, ctx.Err()
		}
		if err := r.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		<-started
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-stopped:
		default:
			t.Error("Close returned before the retry open stopped")
		}
	})
}

func TestRegistryRetiresOldClientsBeforeRetryingCIDExhaustion(t *testing.T) {
	for _, retryExhausts := range []bool{false, true} {
		name := "retry succeeds"
		if retryExhausts {
			name = "retry exhausts remaining budget"
		}
		t.Run(name, func(t *testing.T) {
			device := qmiRegistryDevice("/dev/old-control", "/sys/fake-modem")
			changed := withRegistryControlPath(device, "/dev/new-control")
			closed := false
			old := &Modem{
				deviceInfo: device, deviceKey: physicalDeviceKey(device), generation: 1,
				EquipmentIdentifier: "fake-imei", watchCancel: func() { closed = true },
			}
			opens := 0
			r := &Registry{
				modems: map[string]*Modem{old.Path(): old}, nextGeneration: 1,
				open: func(_ context.Context, d wwanmodem.Device, generation uint64) (*Modem, error) {
					opens++
					if opens == 1 {
						return nil, qcom.QMIErrorClientIDsExhausted
					}
					if !closed {
						t.Error("CID retry began before the old clients closed")
					}
					if retryExhausts {
						return nil, qcom.QMIErrorClientIDsExhausted
					}
					return &Modem{deviceInfo: d, deviceKey: physicalDeviceKey(d), generation: generation, EquipmentIdentifier: "fake-imei"}, nil
				},
			}
			defer func() {
				if err := r.Close(); err != nil {
					t.Error(err)
				}
			}()
			r.applyDeviceEvent(t.Context(), wwanmodem.DeviceEvent{Type: wwanmodem.DeviceChanged, Device: changed})
			if opens != 2 || !closed {
				t.Fatalf("open attempts = %d, old clients closed = %t", opens, closed)
			}
			if retryExhausts {
				if r.cidRecoveryState(old.Path()) != cidRecoverySuspended || len(r.modems) != 0 || r.hasPendingOpens() {
					t.Error("CID exhaustion did not suspend recovery")
				}
				return
			}
			if replacement := r.modems[old.Path()]; replacement == nil || replacement == old || replacement.Generation() <= old.Generation() {
				t.Errorf("replacement = %v, want a new generation", replacement)
			}
		})
	}
}
