package esim

import (
	"context"
	"errors"
	"fmt"
	"time"

	sgp22 "github.com/damonto/euicc-go/v2"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const reminderCleanupTimeout = 15 * time.Second

// EnableProfile idempotently enables a profile and waits for SIM readiness.
func (s *Service) EnableProfile(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID) error {
	sessionCtx, cancel := context.WithTimeout(ctx, enableTimeout)
	defer cancel()

	session, err := s.lifecycle.PrepareEnable(sessionCtx, modem, seID, iccid)
	if err != nil {
		if errors.Is(err, ErrProfileAlreadyActive) {
			return nil
		}
		return err
	}
	defer session.Close()

	if err := s.restoreInternetBeforeProfileEnable(sessionCtx, modem); err != nil {
		return fmt.Errorf("restore internet connection: %w", err)
	}
	return session.Enable(sessionCtx)
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
		if err := s.profile.history.MarkProfileDeletion(ctx, iccid.String(), "deleted"); err != nil {
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
