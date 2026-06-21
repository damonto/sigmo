package modem

import (
	"context"
	"errors"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestMSISDNUpdate(t *testing.T) {
	transientUpdateErr := errors.New("Object does not exist at path \"/org/freedesktop/ModemManager1/Modem/1\"")
	current := &mmodem.Modem{
		EquipmentIdentifier: "354015820228039",
		PrimarySimSlot:      1,
		Sim:                 &mmodem.SIM{Identifier: "8986000000000000000"},
		Ports: []mmodem.ModemPort{
			{
				PortType: mmodem.ModemPortTypeAt,
				Device:   "/dev/ttyUSB2",
			},
		},
	}

	tests := []struct {
		name        string
		number      string
		updateErr   error
		waitErr     error
		wantErr     error
		wantUpdate  bool
		wantRefresh bool
	}{
		{
			name:        "update succeeds after SIM refresh",
			number:      "+1234567890",
			wantUpdate:  true,
			wantRefresh: true,
		},
		{
			name:        "wait timeout after update and SIM refresh",
			number:      "+1234567890",
			waitErr:     context.DeadlineExceeded,
			wantErr:     context.DeadlineExceeded,
			wantUpdate:  true,
			wantRefresh: true,
		},
		{
			name:       "return transient update error without SIM refresh",
			number:     "+1234567890",
			updateErr:  transientUpdateErr,
			wantErr:    transientUpdateErr,
			wantUpdate: true,
		},
		{
			name:    "reject invalid phone number",
			number:  "abc",
			wantErr: errMSISDNInvalidNumber,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMSISDNClient{updateErr: tt.updateErr}
			var refreshCalled bool
			service := &msisdn{
				newClient: func(device string) (msisdnClient, error) {
					if device != "/dev/ttyUSB2" {
						t.Fatalf("device = %q, want /dev/ttyUSB2", device)
					}
					return client, nil
				},
				refreshSIM: func(ctx context.Context, modem *mmodem.Modem, target mmodem.SIMTarget) (*mmodem.Modem, error) {
					_ = ctx.Err()
					refreshCalled = true
					if modem != current {
						t.Fatalf("refresh SIM modem = %p, want %p", modem, current)
					}
					if target.Slot != 1 || target.ICCID != "8986000000000000000" {
						t.Fatalf("refresh SIM target = %+v, want slot 1 ICCID", target)
					}
					if tt.waitErr != nil {
						return nil, tt.waitErr
					}
					return &mmodem.Modem{EquipmentIdentifier: modem.EquipmentIdentifier}, nil
				},
			}

			err := service.Update(context.Background(), current, tt.number)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Update() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			if client.updated != tt.wantUpdate {
				t.Fatalf("client updated = %v, want %v", client.updated, tt.wantUpdate)
			}
			if refreshCalled != tt.wantRefresh {
				t.Fatalf("refresh called = %v, want %v", refreshCalled, tt.wantRefresh)
			}
			if tt.wantUpdate && !client.closed {
				t.Fatalf("client closed = false, want true")
			}
		})
	}
}

type fakeMSISDNClient struct {
	updated   bool
	closed    bool
	updateErr error
}

func (f *fakeMSISDNClient) Update(string, string) error {
	f.updated = true
	return f.updateErr
}

func (f *fakeMSISDNClient) Close() error {
	f.closed = true
	return nil
}
