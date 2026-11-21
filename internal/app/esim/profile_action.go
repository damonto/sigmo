package esim

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	sgp22 "github.com/damonto/euicc-go/v2"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const reminderCleanupTimeout = 15 * time.Second

// EnableProfile idempotently enables a profile and waits for SIM readiness.
func (s *Service) EnableProfile(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID) error {
	return s.enableProfile(ctx, modem, seID, iccid, nil)
}

// AutomationSwitch pins a reversible change to its original SIM slot and eUICC.
type AutomationSwitch struct {
	SEID      string
	EID       string
	FromICCID string
	ToICCID   string
	Slot      uint32
}

// ErrAutomationSIMChanged indicates the expected SIM or secure element has changed.
var ErrAutomationSIMChanged = errors.New("automation SIM has changed")

// SwitchForAutomation checks the old profile while holding the LPA reservation.
// beforeChange journals the restoration and must honor the operation context.
func (s *Service) SwitchForAutomation(ctx context.Context, modem *mmodem.Modem, change AutomationSwitch, beforeChange func(context.Context, *mmodem.Modem) error) error {
	iccid, err := sgp22.NewICCID(change.ToICCID)
	if err != nil {
		return fmt.Errorf("parse automation profile: %w", err)
	}
	return s.enableProfile(ctx, modem, change.SEID, iccid, func(ctx context.Context, current *mmodem.Modem, session *enableSession) error {
		identity := current.SIMIdentity()
		if session == nil || identity.ICCID == "" || !wwan.ICCIDMatches(identity.ICCID, change.FromICCID) || identity.Slot != change.Slot ||
			change.EID == "" || hex.EncodeToString(session.eid) != change.EID || !session.previousEnabled {
			return ErrAutomationSIMChanged
		}
		return beforeChange(ctx, current)
	})
}

type enableGuard func(context.Context, *mmodem.Modem, *enableSession) error

func (s *Service) enableProfile(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID, guard enableGuard) error {
	sessionCtx, cancel := context.WithTimeout(ctx, enableTimeout)
	defer cancel()

	var session *enableSession
	var active *mmodem.Modem
	err := s.registry.WithSIMChange(sessionCtx, modem, func(ctx context.Context, current *mmodem.Modem) error {
		var err error
		session, err = s.lifecycle.PrepareEnable(ctx, current, seID, iccid)
		if errors.Is(err, ErrProfileAlreadyActive) {
			if guard != nil {
				return ErrAutomationSIMChanged
			}
			return nil
		}
		if err != nil {
			return err
		}
		defer session.Close()
		if guard != nil {
			if err := guard(ctx, current, session); err != nil {
				return err
			}
		}
		if err := s.restoreInternetBeforeProfileEnable(ctx, current); err != nil {
			return fmt.Errorf("restore internet connection: %w", err)
		}
		active, err = session.Enable(ctx)
		return err
	})
	if err != nil {
		return err
	}
	// Notification delivery is best effort and does not keep other requests
	// waiting after the enabled profile and replacement modem are confirmed.
	if active != nil {
		session.notify(sessionCtx, active, s.registry)
	}
	return nil
}

// DeleteProfile records deletion intent, updates the card, and cleans up reminders.
func (s *Service) DeleteProfile(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID) error {
	session, err := s.lifecycle.PrepareDelete(ctx, modem, seID, iccid)
	if errors.Is(err, ErrProfileNotFound) {
		// Absence confirms only this location; a stale URL must not move history
		// or undo an assignment after the profile has moved elsewhere.
		if s.profile != nil && s.profile.history != nil {
			if err := s.profile.history.ConfirmESIMProfileDeletion(ctx, iccid.String(), modem.EquipmentIdentifier, seID); err != nil {
				return err
			}
		}
		return s.deleteProfileReminder(ctx, modem, seID, iccid)
	}
	if err != nil {
		return err
	}
	defer session.Close()

	if s.profile != nil && s.profile.history != nil {
		if err := s.profile.history.RecordSIMProfile(ctx, storage.SIMProfile{ProfileID: iccid.String(), ModemID: modem.EquipmentIdentifier, Kind: "esim", SEID: seID}); err != nil {
			return err
		}
		if err := s.profile.history.MarkProfileDeletion(ctx, iccid.String(), "pending"); err != nil {
			return err
		}
	}
	if err := session.Delete(); err != nil {
		return err
	}
	if s.profile != nil && s.profile.history != nil {
		if err := s.profile.history.ConfirmESIMProfileDeletion(ctx, iccid.String(), modem.EquipmentIdentifier, seID); err != nil {
			return err
		}
	}
	return s.deleteProfileReminder(ctx, modem, seID, iccid)
}

func (s *Service) deleteProfileReminder(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID) error {
	if s.reminders == nil {
		return nil
	}

	cleanupCtx, cancel := context.WithTimeout(ctx, reminderCleanupTimeout)
	defer cancel()
	if err := s.reminders.DeleteForProfile(cleanupCtx, reminder.ProfileTypeESIM, iccid.String(), modem.EquipmentIdentifier, seID); err != nil {
		if errors.Is(err, reminder.ErrProfileMismatch) {
			return ErrProfileNotFound
		}
		return fmt.Errorf("delete profile reminder: %w", err)
	}
	return nil
}

// ProfileInventory reads installed profiles for an already resolved modem.
func (s *Service) ProfileInventory(ctx context.Context, modem *mmodem.Modem) (*Inventory, error) {
	return s.profile.List(ctx, modem)
}
