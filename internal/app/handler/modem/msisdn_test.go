package modem

import (
	"context"
	"errors"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/config"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestMSISDNUpdate(t *testing.T) {
	transientUpdateErr := errors.New("Object does not exist at path \"/org/freedesktop/ModemManager1/Modem/1\"")
	current := &mmodem.Modem{
		EquipmentIdentifier: "354015820228039",
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
		wantRestart bool
	}{
		{
			name:        "update succeeds after modem wait",
			number:      "+1234567890",
			wantUpdate:  true,
			wantRestart: true,
		},
		{
			name:        "wait timeout after update and restart",
			number:      "+1234567890",
			waitErr:     context.DeadlineExceeded,
			wantErr:     context.DeadlineExceeded,
			wantUpdate:  true,
			wantRestart: true,
		},
		{
			name:       "return transient update error without restart",
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
			var restartCalled bool
			service := &msisdn{
				store: config.NewStore(config.Default()),
				newClient: func(device string) (msisdnClient, error) {
					if device != "/dev/ttyUSB2" {
						t.Fatalf("device = %q, want /dev/ttyUSB2", device)
					}
					return client, nil
				},
				restartModem: func(modem *mmodem.Modem, compatible bool) error {
					restartCalled = true
					if modem != current {
						t.Fatalf("restart modem = %p, want %p", modem, current)
					}
					if compatible {
						t.Fatalf("compatible = true, want false")
					}
					return nil
				},
				waitForModem: func(ctx context.Context, modem *mmodem.Modem, action func() error) (*mmodem.Modem, error) {
					if err := action(); err != nil {
						return nil, err
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
			if restartCalled != tt.wantRestart {
				t.Fatalf("restart called = %v, want %v", restartCalled, tt.wantRestart)
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
