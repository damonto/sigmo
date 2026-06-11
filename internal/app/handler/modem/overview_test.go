package modem

import (
	"context"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

func TestCatalogBuildResponseLockedModem(t *testing.T) {
	tests := []struct {
		name            string
		lock            mmodem.ModemLock
		wantSupported   bool
		wantUnlockLabel string
	}{
		{
			name:            "supports sim pin unlock",
			lock:            mmodem.ModemLockSimPin,
			wantSupported:   true,
			wantUnlockLabel: "sim-pin",
		},
		{
			name:            "reports unsupported puk lock",
			lock:            mmodem.ModemLockSimPuk,
			wantUnlockLabel: "sim-puk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)
			device := &mmodem.Modem{
				EquipmentIdentifier: "860588043408833",
				Manufacturer:        "Quectel",
				Model:               "RM520N",
				State:               mmodem.ModemStateLocked,
				UnlockRequired:      tt.lock,
			}

			got, err := catalog.buildResponse(context.Background(), device)
			if err != nil {
				t.Fatalf("buildResponse() error = %v", err)
			}
			if got.State != "locked" {
				t.Fatalf("state = %q, want locked", got.State)
			}
			if got.UnlockRequired != tt.wantUnlockLabel {
				t.Fatalf("unlockRequired = %q, want %q", got.UnlockRequired, tt.wantUnlockLabel)
			}
			if got.UnlockSupported != tt.wantSupported {
				t.Fatalf("unlockSupported = %v, want %v", got.UnlockSupported, tt.wantSupported)
			}
		})
	}
}
