package communication

import (
	"context"
	"net/http"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/carrier"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/labstack/echo/v5"
)

func (h *Handler) inventory(ctx context.Context) ([]storage.SIMLine, error) {
	baseline, err := h.config.Storage.SIMLines(ctx)
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
	if h.config.Registry == nil {
		return h.config.Storage.SIMLines(ctx)
	}
	modems, err := h.config.Registry.Modems(ctx)
	if err != nil {
		// Cached histories remain readable while the hardware inventory is unavailable.
		return h.config.Storage.SIMLines(ctx)
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
			if err := h.config.Storage.RecordSIMProfile(ctx, storage.SIMProfile{ProfileID: slot.Identifier, ModemID: device.EquipmentIdentifier, Kind: kind, Region: carrier.Lookup(slot.OperatorIdentifier).Region}); err != nil {
				return nil, err
			}
			present[slot.Identifier] = true
		}
		if snapshot.SIM != nil && snapshot.SIM.Identifier != "" {
			active[snapshot.SIM.Identifier] = !snapshot.Locked()
			present[snapshot.SIM.Identifier] = true
			if err := h.config.Storage.RecordSIMProfile(ctx, storage.SIMProfile{ProfileID: snapshot.SIM.Identifier, ModemID: device.EquipmentIdentifier, Number: snapshot.Number, Region: carrier.Lookup(snapshot.SIM.OperatorIdentifier).Region}); err != nil {
				return nil, err
			}
		}
		if h.config.ESIM != nil && snapshot.SIMKind() == "euicc" && !snapshot.Locked() {
			profiles, err := h.config.ESIM.ProfileInventory(ctx, device)
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
	lines, err := h.config.Storage.SIMLines(ctx)
	if err != nil {
		return nil, err
	}
	for i := range lines {
		line := &lines[i]
		line.Present = present[line.ProfileID]
		line.Active = active[line.ProfileID]
		line.CanMessage = line.Active
		line.CanCall = line.Active && h.config.CallsEnabled
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
			changed, err := h.config.Storage.ReconcileProfileDeletion(ctx, storage.ProfileReconciliation{ProfileID: line.ProfileID, ExpectedState: before.DeletionState, ExpectedSuccessor: before.SuccessorID, NewState: state})
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

func (h *Handler) lines(c *echo.Context) error {
	var lines []storage.SIMLine
	var err error
	if c.QueryParam("cached") == "true" {
		lines, err = h.config.Storage.SIMLines(c.Request().Context())
	} else {
		lines, err = h.inventory(c.Request().Context())
	}
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, struct {
		Items []storage.SIMLine `json:"items"`
	}{lines})
}

func (h *Handler) assign(c *echo.Context) error {
	var body struct {
		TargetProfileID string `json:"targetProfileId"`
	}
	if err := httpapi.BindJSON(c, &body, "history_assignment_invalid"); err != nil {
		return err
	}
	lines, err := h.inventory(c.Request().Context())
	if err != nil {
		return writeError(c, err)
	}
	valid := false
	for _, line := range lines {
		if line.ProfileID == body.TargetProfileID && line.Present && line.DeletionState == "" {
			valid = true
		}
	}
	if !valid {
		return writeError(c, storage.ErrInvalidAssignment)
	}
	if err := h.config.Storage.AssignHistory(c.Request().Context(), storage.HistoryAssignment{SourceProfileID: c.Param("profileId"), TargetProfileID: body.TargetProfileID}); err != nil {
		return writeError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
