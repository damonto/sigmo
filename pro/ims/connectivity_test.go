//go:build ims

package ims

import (
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestResolveWiFiCallingSettingsUsesReplacementResource(t *testing.T) {
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	got, err := ResolveWiFiCallingSettings(modem, WiFiCallingSettings{
		Underlay: UnderlaySettings{Mode: UnderlayModeSystem},
	})
	if err != nil {
		t.Fatalf("ResolveWiFiCallingSettings() error = %v", err)
	}
	want := WiFiCallingSettings{Underlay: UnderlaySettings{Mode: UnderlayModeSystem}}
	if got != want {
		t.Fatalf("ResolveWiFiCallingSettings() = %+v, want %+v", got, want)
	}
}

func TestConnectivityLocksChangesPerModem(t *testing.T) {
	connectivity := &Connectivity{}
	unlockFirst := connectivity.lockModem("modem-1")

	sameModemAcquired := make(chan struct{})
	go func() {
		unlock := connectivity.lockModem("modem-1")
		close(sameModemAcquired)
		unlock()
	}()

	select {
	case <-sameModemAcquired:
		t.Fatal("second change acquired the same modem lock")
	case <-time.After(20 * time.Millisecond):
	}

	unlockOther := connectivity.lockModem("modem-2")
	unlockOther()
	unlockFirst()

	select {
	case <-sameModemAcquired:
	case <-time.After(time.Second):
		t.Fatal("second change did not acquire the released modem lock")
	}
}
