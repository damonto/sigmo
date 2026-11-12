// Package simprofile reconciles live SIM inventory with communication history.
package simprofile

import (
	"context"

	appesim "github.com/damonto/sigmo/internal/app/esim"
	"github.com/damonto/sigmo/internal/pkg/carrier"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type modemInventory interface {
	Modems(context.Context) (map[string]*modem.Modem, error)
}

type profileInventory interface {
	ProfileInventory(context.Context, *modem.Modem) (*appesim.Inventory, error)
}

// Service owns SIM availability and the validity of history handovers.
type Service struct {
	store    *storage.Store
	registry modemInventory
	esim     profileInventory
}

// New creates an inventory service; absent live sources retain access to cached history.
func New(store *storage.Store, registry *modem.Registry, esim *appesim.Service) *Service {
	s := &Service{store: store}
	if registry != nil {
		s.registry = registry
	}
	if esim != nil {
		s.esim = esim
	}
	return s
}

// List returns cached profiles or reconciles them with the live inventory.
func (s *Service) List(ctx context.Context, refresh bool) ([]storage.SIMLine, error) {
	if !refresh {
		return s.store.SIMLines(ctx)
	}
	return s.inventory(ctx)
}

// Preference returns the preferred SIM profile for a communication participant.
func (s *Service) Preference(ctx context.Context, participant string) (storage.CommunicationPreference, error) {
	return s.store.PreferredCommunication(ctx, participant)
}

// AssignHistory validates the current target before assigning archived history.
func (s *Service) AssignHistory(ctx context.Context, sourceID, targetID string) error {
	lines, err := s.inventory(ctx)
	if err != nil {
		return err
	}
	valid := false
	for _, line := range lines {
		if line.ProfileID == targetID && line.Present && line.DeletionState == "" {
			valid = true
		}
	}
	if !valid {
		return storage.ErrInvalidAssignment
	}
	if err := s.store.AssignHistory(ctx, storage.HistoryAssignment{SourceProfileID: sourceID, TargetProfileID: targetID}); err != nil {
		return err
	}
	return nil
}

func (s *Service) inventory(ctx context.Context) ([]storage.SIMLine, error) {
	baseline, err := s.store.SIMLines(ctx)
	if err != nil {
		return nil, err
	}
	observed := make(map[string]storage.SIMLine, len(baseline))
	for _, line := range baseline {
		observed[line.ProfileID] = line
	}
	present := make(map[string]bool)
	active := make(map[string]bool)
	confirmed := make(map[string]bool)
	if s.registry == nil {
		return s.store.SIMLines(ctx)
	}
	modems, err := s.registry.Modems(ctx)
	if err != nil {
		// Cached histories remain readable while the hardware inventory is unavailable.
		return s.store.SIMLines(ctx)
	}
	for _, device := range modems {
		snapshot := device.Snapshot()
		for _, slot := range snapshot.Slots {
			if slot == nil || slot.Identifier == "" {
				continue
			}
			kind := ""
			// The active slot's ATR cannot classify other slots.
			if snapshot.SIM != nil && snapshot.SIM.Identifier == slot.Identifier {
				switch snapshot.SIMKind() {
				case "euicc":
					kind = "esim"
				case "physical":
					kind = "physical"
				}
			}
			if err := s.store.RecordSIMProfile(ctx, storage.SIMProfile{ProfileID: slot.Identifier, ModemID: device.EquipmentIdentifier, Kind: kind, Region: carrier.Lookup(slot.OperatorIdentifier).Region}); err != nil {
				return nil, err
			}
			present[slot.Identifier] = true
		}
		if snapshot.SIM != nil && snapshot.SIM.Identifier != "" {
			active[snapshot.SIM.Identifier] = !snapshot.Locked()
			present[snapshot.SIM.Identifier] = true
			if err := s.store.RecordSIMProfile(ctx, storage.SIMProfile{ProfileID: snapshot.SIM.Identifier, ModemID: device.EquipmentIdentifier, Number: snapshot.Number, Region: carrier.Lookup(snapshot.SIM.OperatorIdentifier).Region}); err != nil {
				return nil, err
			}
		}
		if s.esim != nil && snapshot.SIMKind() == "euicc" && !snapshot.Locked() {
			profiles, err := s.esim.ProfileInventory(ctx, device)
			if err != nil {
				continue
			} // Cached lines remain visible but cannot become handover targets.
			for _, group := range profiles.SEs {
				confirmed[device.EquipmentIdentifier+"\x00"+group.ID] = true
				for _, profile := range group.Profiles {
					present[profile.ICCID] = true
				}
			}
		}
	}
	lines, err := s.store.SIMLines(ctx)
	if err != nil {
		return nil, err
	}
	for i := range lines {
		line := &lines[i]
		line.Present = present[line.ProfileID]
		line.Active = active[line.ProfileID]
		line.CanMessage = line.Active
		line.CanCall = line.Active
		before, known := observed[line.ProfileID]
		if !known || before.DeletionState != line.DeletionState || before.SuccessorID != line.SuccessorID {
			// This inventory started before the latest operation. The next poll can reconcile it.
			line.Present = line.Present && line.DeletionState == ""
			line.Active = line.Active && line.Present
			line.CanMessage = line.CanMessage && line.Present
			line.CanCall = line.CanCall && line.Present
			continue
		}
		state := line.DeletionState
		if line.Present && (state == "deleted" || state == "assigned") {
			state = ""
		}
		if !line.Present && state == "pending" && confirmed[line.ModemID+"\x00"+line.SEID] {
			state = "deleted"
		}
		if state != line.DeletionState {
			changed, err := s.store.ReconcileProfileDeletion(ctx, storage.ProfileReconciliation{ProfileID: line.ProfileID, ExpectedState: before.DeletionState, ExpectedSuccessor: before.SuccessorID, NewState: state})
			if err != nil {
				return nil, err
			}
			if changed {
				line.DeletionState = state
				if state == "" {
					line.SuccessorID = ""
				}
			} else {
				// Do not advertise a handover target using a superseded inventory.
				line.Present, line.Active, line.CanMessage, line.CanCall = false, false, false, false
			}
		}
	}
	return lines, nil
}
