package esim

import (
	"context"
	"errors"
	"log/slog"

	sgp22 "github.com/damonto/euicc-go/v2"

	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func (s *Service) Enable(ctx context.Context, modem *mmodem.Modem, iccid sgp22.ICCID) error {
	client, err := lpa.New(modem, s.cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	closeClient := func() {
		if client == nil {
			return
		}
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
		client = nil
	}
	defer closeClient()

	notifications, err := client.ListNotification()
	if err != nil {
		slog.Error("failed to list notifications", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	var lastSeq sgp22.SequenceNumber
	for _, notification := range notifications {
		lastSeq = max(lastSeq, notification.SequenceNumber)
	}

	if err := client.EnableProfile(iccid, true); err != nil {
		slog.Error("failed to enable profile", "modem", modem.EquipmentIdentifier, "iccid", iccid.String(), "error", err)
		return err
	}

	closeClient()

	if err := modem.Restart(s.cfg.FindModem(modem.EquipmentIdentifier).Compatible); err != nil {
		slog.Error("failed to restart modem", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}

	target, err := s.manager.WaitForModem(ctx, modem.EquipmentIdentifier)
	if err != nil {
		slog.Error("failed to wait for modem", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	if err := s.sendPendingNotifications(target, lastSeq); err != nil {
		slog.Warn("failed to handle modem notifications", "error", err, "modem", modem.EquipmentIdentifier)
	}
	return nil
}

func (s *Service) Delete(modem *mmodem.Modem, iccid sgp22.ICCID) error {
	client, err := lpa.New(modem, s.cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()

	if err := client.Delete(iccid); err != nil {
		slog.Error("failed to delete profile", "modem", modem.EquipmentIdentifier, "iccid", iccid.String(), "error", err)
		return err
	}
	return nil
}

func (s *Service) sendPendingNotifications(modem *mmodem.Modem, lastSeq sgp22.SequenceNumber) error {
	client, err := lpa.New(modem, s.cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()
	notifications, err := client.ListNotification()
	if err != nil {
		slog.Error("failed to list notifications", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	var errs error
	for _, notification := range notifications {
		if notification.SequenceNumber <= lastSeq {
			continue
		}
		if err := client.SendNotification(notification.SequenceNumber, true); err != nil {
			slog.Error("failed to send notification", "sequence", notification.SequenceNumber, "error", err)
			errs = errors.Join(errs, err)
		}
	}
	return errs
}
