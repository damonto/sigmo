package modem

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	wwanmodem "github.com/damonto/wwan-go/modem"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestMSISDNUpdate(t *testing.T) {
	for _, tt := range []struct {
		name      string
		number    string
		nativeErr error
		atErr     error
		saveFails bool
		wantErr   error
		wantWrite bool
	}{
		{name: "native write", number: "+12025550123", wantWrite: true},
		{name: "card refuses write", number: "+12025550123", nativeErr: errors.New("access denied"), wantWrite: true},
		{name: "card write times out", number: "+12025550123", nativeErr: context.DeadlineExceeded, wantWrite: true},
		{name: "AT fallback", number: "+12025550123", nativeErr: wwan.ErrUnsupported, wantWrite: true},
		{name: "AT fallback is also best effort", number: "+12025550123", nativeErr: wwan.ErrUnsupported, atErr: errors.New("access denied"), wantWrite: true},
		{name: "invalid number", number: "abc", wantErr: errMSISDNInvalidNumber},
		{name: "storage failure stops card write", number: "+12025550123", saveFails: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store := newTestNumberStore(t)
			current := &mmodem.Modem{
				EquipmentIdentifier: "device",
				PrimarySIMSlot:      1,
				SIM:                 &mmodem.SIM{Identifier: "sim-a"},
				Number:              "+12025550100",
				Ports:               []mmodem.ModemPort{{PortType: wwanmodem.PortAT, Device: "/dev/ttyUSB2"}},
			}
			device := &fakeMSISDNDevice{err: tt.nativeErr}
			at := &fakeMSISDNClient{err: tt.atErr}
			service := &msisdn{
				store: store,
				openDevice: func(*mmodem.Modem) (msisdnDevice, error) {
					numbers, err := store.SIMProfileNumbers(ctx, []string{"sim-a"})
					if err != nil || numbers["sim-a"] != tt.number {
						t.Fatalf("store before writing card = %v, %v", numbers, err)
					}
					return device, nil
				},
				newClient: func(ctx context.Context, port string) (msisdnClient, error) {
					if _, ok := ctx.Deadline(); !ok || port != "/dev/ttyUSB2" {
						t.Fatalf("AT port = %q, has deadline = %v", port, ok)
					}
					return at, nil
				},
			}
			if tt.saveFails {
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			}

			err := service.Update(ctx, current, tt.number)
			if tt.saveFails {
				if err == nil {
					t.Fatal("Update succeeded with a closed database")
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Update() error = %v, want %v", err, tt.wantErr)
			}
			wantNumber := "+12025550100"
			if tt.wantWrite {
				wantNumber = tt.number
			}
			if got := current.Snapshot().Number; got != wantNumber {
				t.Errorf("effective number = %q, want %q", got, wantNumber)
			}
			if (device.number != "") != tt.wantWrite {
				t.Errorf("native write = %q, want attempt = %v", device.number, tt.wantWrite)
			}
			if errors.Is(tt.nativeErr, wwan.ErrUnsupported) && (at.number != tt.number || !at.closed) {
				t.Errorf("AT fallback wrote %q, closed = %v", at.number, at.closed)
			}
		})
	}
}

func TestMSISDNUpdateKeepsNumberWithOriginalSIM(t *testing.T) {
	store := newTestNumberStore(t)
	current := &mmodem.Modem{EquipmentIdentifier: "device", PrimarySIMSlot: 1, SIM: &mmodem.SIM{Identifier: "sim-a"}}
	device := &fakeMSISDNDevice{onUpdate: func() {
		current.InvalidateSIMActivation()
		current.SIM = &mmodem.SIM{Identifier: "sim-b"}
	}}
	service := &msisdn{
		store:      store,
		openDevice: func(*mmodem.Modem) (msisdnDevice, error) { return device, nil },
	}
	if err := service.Update(t.Context(), current, "+12025550123"); err != nil {
		t.Fatal(err)
	}
	numbers, err := store.SIMProfileNumbers(t.Context(), []string{"sim-a", "sim-b"})
	if err != nil || numbers["sim-a"] != "+12025550123" || numbers["sim-b"] != "" {
		t.Fatalf("stored numbers = %v, %v", numbers, err)
	}
	if got := current.Snapshot().Number; got != "" {
		t.Fatalf("replacement SIM number = %q", got)
	}
}

func newTestNumberStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "sim.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

type fakeMSISDNDevice struct {
	err      error
	number   string
	onUpdate func()
}

func (f *fakeMSISDNDevice) UpdateMSISDN(_ context.Context, number string) error {
	f.number = number
	if f.onUpdate != nil {
		f.onUpdate()
	}
	return f.err
}

type fakeMSISDNClient struct {
	number string
	closed bool
	err    error
}

func (f *fakeMSISDNClient) Update(_, number string) error {
	f.number = number
	return f.err
}

func (f *fakeMSISDNClient) Close() error {
	f.closed = true
	return nil
}
