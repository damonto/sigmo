package esim

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sgp22 "github.com/damonto/euicc-go/v2"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
)

const reminderCleanupTimeout = 15 * time.Second

func (h *Handler) EnableProfile(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID) error {
	session, err := h.lifecycle.PrepareEnable(modem, seID, iccid)
	if err != nil {
		if errors.Is(err, errProfileAlreadyActive) {
			return nil
		}
		return err
	}
	defer session.Close()

	sessionCtx, cancel := context.WithTimeout(ctx, enableTimeout)
	defer cancel()
	if err := h.restoreInternetBeforeProfileEnable(sessionCtx, modem); err != nil {
		return fmt.Errorf("restore internet connection: %w", err)
	}
	return session.Enable(sessionCtx)
}

func (h *Handler) DeleteProfile(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID) error {
	deleteErr := h.lifecycle.Delete(modem, seID, iccid)
	if deleteErr != nil && !errors.Is(deleteErr, errProfileNotFound) {
		return deleteErr
	}
	if h.reminders == nil {
		return deleteErr
	}

	cleanupCtx, cancel := context.WithTimeout(ctx, reminderCleanupTimeout)
	defer cancel()
	if errors.Is(deleteErr, errProfileNotFound) {
		stored, ok, err := h.reminders.Get(cleanupCtx, reminder.ProfileTypeESIM, iccid.String())
		if err != nil {
			return fmt.Errorf("read profile reminder for cleanup: %w", err)
		}
		if !ok {
			return nil
		}
		if stored.ModemID != modem.EquipmentIdentifier || strings.TrimSpace(stored.SEID) != strings.TrimSpace(seID) {
			return deleteErr
		}
	}
	if err := h.reminders.Delete(cleanupCtx, reminder.ProfileTypeESIM, iccid.String()); err != nil {
		return fmt.Errorf("delete profile reminder: %w", err)
	}
	return nil
}
