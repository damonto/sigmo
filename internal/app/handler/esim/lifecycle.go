package esim

import (
	"bytes"
	"context"
	"errors"
	"log/slog"

	sgp22 "github.com/damonto/euicc-go/v2"

	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type lifecycle struct {
	cfg     *config.Config
	manager *mmodem.Manager
}

type enableSession struct {
	l       *lifecycle
	modem   *mmodem.Modem
	iccid   sgp22.ICCID
	client  *lpa.LPA
	lastSeq sgp22.SequenceNumber
}

var (
	errActiveProfileCannotDelete = errors.New("active profile cannot be deleted")
	errProfileNotFound           = errors.New("profile not found")
	errProfileAlreadyActive      = errors.New("profile already active")
)

func newLifecycle(cfg *config.Config, manager *mmodem.Manager) *lifecycle {
	return &lifecycle{
		cfg:     cfg,
		manager: manager,
	}
}

func (l *lifecycle) PrepareEnable(modem *mmodem.Modem, iccid sgp22.ICCID) (*enableSession, error) {
	client, err := lpa.New(modem, l.cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
	}
	session := &enableSession{
		l:      l,
		modem:  modem,
		iccid:  iccid,
		client: client,
	}
	cleanup := func(err error) (*enableSession, error) {
		session.Close()
		return nil, err
	}

	profiles, err := client.ListProfile(nil, nil)
	if err != nil {
		slog.Error("failed to list profiles", "modem", modem.EquipmentIdentifier, "error", err)
		return cleanup(err)
	}
	profile, ok := profileByICCID(profiles, iccid)
	if !ok {
		return cleanup(errProfileNotFound)
	}
	if profile.ProfileState == sgp22.ProfileEnabled {
		return cleanup(errProfileAlreadyActive)
	}

	notifications, err := client.ListNotification()
	if err != nil {
		slog.Error("failed to list notifications", "modem", modem.EquipmentIdentifier, "error", err)
		return cleanup(err)
	}
	for _, notification := range notifications {
		session.lastSeq = max(session.lastSeq, notification.SequenceNumber)
	}
	return session, nil
}

func (s *enableSession) Enable(ctx context.Context) error {
	defer s.Close()

	if err := s.client.EnableProfile(s.iccid, true); err != nil {
		slog.Error("failed to enable profile", "modem", s.modem.EquipmentIdentifier, "iccid", s.iccid.String(), "error", err)
		return err
	}

	s.Close()

	if err := s.modem.Restart(s.l.cfg.FindModem(s.modem.EquipmentIdentifier).Compatible); err != nil {
		slog.Error("failed to restart modem", "modem", s.modem.EquipmentIdentifier, "error", err)
		return err
	}

	target, err := s.l.manager.WaitForModem(ctx, s.modem)
	if err != nil {
		slog.Error("failed to wait for modem", "modem", s.modem.EquipmentIdentifier, "error", err)
		return err
	}
	if err := s.l.sendPendingNotifications(target, s.lastSeq); err != nil {
		slog.Warn("failed to handle modem notifications", "error", err, "modem", s.modem.EquipmentIdentifier)
	}
	return nil
}

func (s *enableSession) Close() {
	if s == nil || s.client == nil {
		return
	}
	if err := s.client.Close(); err != nil {
		slog.Warn("failed to close LPA client", "error", err)
	}
	s.client = nil
}

func (l *lifecycle) Delete(modem *mmodem.Modem, iccid sgp22.ICCID) error {
	client, err := lpa.New(modem, l.cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()

	profiles, err := client.ListProfile(nil, nil)
	if err != nil {
		slog.Error("failed to list profiles", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	if activeProfile(profiles, iccid) {
		return errActiveProfileCannotDelete
	}

	if err := client.Delete(iccid); err != nil {
		slog.Error("failed to delete profile", "modem", modem.EquipmentIdentifier, "iccid", iccid.String(), "error", err)
		return err
	}
	return nil
}

func activeProfile(profiles []*sgp22.ProfileInfo, iccid sgp22.ICCID) bool {
	profile, ok := profileByICCID(profiles, iccid)
	return ok && profile.ProfileState == sgp22.ProfileEnabled
}

func profileByICCID(profiles []*sgp22.ProfileInfo, iccid sgp22.ICCID) (*sgp22.ProfileInfo, bool) {
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		if bytes.Equal(profile.ICCID, iccid) {
			return profile, true
		}
	}
	return nil, false
}

func (l *lifecycle) sendPendingNotifications(modem *mmodem.Modem, lastSeq sgp22.SequenceNumber) error {
	client, err := lpa.New(modem, l.cfg)
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
