package modem

import (
	"errors"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestSIMSlotTargetIndexUsesPhysicalSlot(t *testing.T) {
	modem := &mmodem.Modem{
		PrimarySimSlot: 1,
		SimSlots:       []uint32{1, 2},
		Sim:            &mmodem.SIM{Slot: 1, Active: true, Identifier: "duplicate-iccid"},
	}
	target := newSIMSlot(nil)
	got, err := target.targetIndex(modem, 2)
	if err != nil {
		t.Fatalf("targetIndex() error = %v", err)
	}
	if got != 2 {
		t.Fatalf("targetIndex() = %d, want 2", got)
	}
	if _, err := target.targetIndex(modem, 1); !errors.Is(err, errSimSlotAlreadyActive) {
		t.Fatalf("active target error = %v, want %v", err, errSimSlotAlreadyActive)
	}
}
