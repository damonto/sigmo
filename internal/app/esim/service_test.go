package esim

import (
	"testing"

	wwanmodem "github.com/damonto/wwan-go/modem"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestRestoreInternetBeforeProfileEnable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modem   *mmodem.Modem
		wantErr bool
	}{
		{
			name: "skip current SIM internet restore while modem is locked",
			modem: &mmodem.Modem{
				Status: wwanmodem.Status{SIM: wwanmodem.SIMStateLocked},
			},
		},
		{
			name:    "unlocked modem requires internet connector",
			modem:   &mmodem.Modem{Status: wwanmodem.Status{Power: wwanmodem.PowerStateOn, Registration: wwanmodem.RegistrationHome}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := &Service{}
			err := h.restoreInternetBeforeProfileEnable(t.Context(), tt.modem)
			if (err != nil) != tt.wantErr {
				t.Fatalf("restoreInternetBeforeProfileEnable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
