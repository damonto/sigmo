package simprofile

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestHistoryAssignmentRequiresAnAvailableTarget(t *testing.T) {
	for _, offline := range []bool{false, true} {
		name := "live target"
		if offline {
			name = "cached target while device unavailable"
		}
		t.Run(name, func(t *testing.T) {
			db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			for _, id := range []string{"source", "target"} {
				if err := db.RecordSIMProfile(t.Context(), storage.SIMProfile{ProfileID: id, ModemID: "modem-1"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.MarkProfileDeletion(t.Context(), "source", "deleted"); err != nil {
				t.Fatal(err)
			}
			registry := &inventorySource{device: &modem.Modem{
				EquipmentIdentifier: "modem-1", SIM: &modem.SIM{Identifier: "target"},
			}}
			if offline {
				registry.err = errors.New("device unavailable")
			}
			service := &Service{store: db, registry: registry}
			cached, err := service.List(t.Context(), false)
			if err != nil || len(cached) != 2 || registry.calls != 0 {
				t.Fatalf("cached inventory = %+v, %v; device reads = %d", cached, err, registry.calls)
			}
			err = service.AssignHistory(t.Context(), "source", "target")
			if offline && !errors.Is(err, storage.ErrInvalidAssignment) {
				t.Fatalf("unavailable target assignment = %v", err)
			}
			if !offline && err != nil {
				t.Fatal(err)
			}
			lines, err := db.SIMLines(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range lines {
				if line.ProfileID != "source" {
					continue
				}
				if offline && (line.DeletionState != "deleted" || line.SuccessorID != "") {
					t.Fatalf("rejected assignment changed source: %+v", line)
				}
				if !offline && (line.DeletionState != "assigned" || line.SuccessorID != "target") {
					t.Fatalf("assignment did not preserve successor: %+v", line)
				}
			}
		})
	}
}

type inventorySource struct {
	device *modem.Modem
	err    error
	calls  int
}

func (s *inventorySource) Modems(ctx context.Context) (map[string]*modem.Modem, error) {
	s.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.err != nil {
		return nil, s.err
	}
	return map[string]*modem.Modem{s.device.EquipmentIdentifier: s.device}, nil
}
