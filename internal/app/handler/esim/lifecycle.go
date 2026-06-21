package esim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/damonto/euicc-go/bertlv"
	sgp22 "github.com/damonto/euicc-go/v2"

	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type lifecycle struct {
	settings         *settings.Settings
	store            *settings.Store
	newClient        lifecycleClientFactory
	ensureSIMVisible func(context.Context, *mmodem.Modem, mmodem.SIMTarget) (*mmodem.Modem, error)
}

type enableSession struct {
	l       *lifecycle
	modem   *mmodem.Modem
	iccid   sgp22.ICCID
	client  lifecycleClient
	lastSeq sgp22.SequenceNumber
}

type lifecycleClient interface {
	ListProfile(any, []bertlv.Tag) ([]*sgp22.ProfileInfo, error)
	ListNotification(...sgp22.NotificationEvent) ([]*sgp22.NotificationMetadata, error)
	EnableProfile(any, bool) error
	Delete(sgp22.ICCID) error
	SendNotification(any, bool) error
	Close() error
}

type lifecycleClientFactory func(*mmodem.Modem, *settings.Settings) (lifecycleClient, error)

var (
	errActiveProfileCannotDelete = errors.New("active profile cannot be deleted")
	errProfileNotFound           = errors.New("profile not found")
	errProfileAlreadyActive      = errors.New("profile already active")
)

func newLifecycle(store *settings.Store, registry *mmodem.Registry) *lifecycle {
	return &lifecycle{
		store:            store,
		newClient:        newLifecycleClient,
		ensureSIMVisible: registry.EnsureSIMVisible,
	}
}

func newLifecycleClient(modem *mmodem.Modem, currentSettings *settings.Settings) (lifecycleClient, error) {
	return lpa.New(modem, currentSettings)
}

func (l *lifecycle) settingsSnapshot() *settings.Settings {
	if l.store != nil {
		currentSettings := l.store.Snapshot()
		return &currentSettings
	}
	if l.settings != nil {
		return l.settings
	}
	return settings.Default()
}

func (l *lifecycle) PrepareEnable(modem *mmodem.Modem, iccid sgp22.ICCID) (*enableSession, error) {
	currentSettings := l.settingsSnapshot()
	client, err := l.newClient(modem, currentSettings)
	if err != nil {
		return nil, fmt.Errorf("create LPA client: %w", err)
	}
	session := &enableSession{
		l:      l,
		modem:  modem,
		iccid:  iccid,
		client: client,
	}
	release := false
	defer func() {
		if !release {
			session.Close()
		}
	}()

	profiles, err := client.ListProfile(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	profile, ok := profileByICCID(profiles, iccid)
	if !ok {
		return nil, errProfileNotFound
	}
	if profile.ProfileState == sgp22.ProfileEnabled {
		return nil, errProfileAlreadyActive
	}

	notifications, err := client.ListNotification()
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	for _, notification := range notifications {
		session.lastSeq = max(session.lastSeq, notification.SequenceNumber)
	}
	release = true
	return session, nil
}

func (s *enableSession) Enable(ctx context.Context) error {
	defer s.Close()

	if err := s.client.EnableProfile(s.iccid, true); err != nil {
		return fmt.Errorf("enable profile %s: %w", s.iccid.String(), err)
	}

	s.Close()

	if err := s.finish(ctx); err != nil {
		return err
	}
	return nil
}

func (s *enableSession) finish(ctx context.Context) error {
	target, err := s.l.ensureSIMVisible(ctx, s.modem, mmodem.SIMTarget{ICCID: s.iccid.String()})
	if err != nil {
		return fmt.Errorf("wait for modem readiness: %w", err)
	}
	if err := s.l.sendPendingNotifications(target, s.lastSeq); err != nil {
		slog.Warn("failed to handle modem notifications", "error", err, "imei", s.modem.EquipmentIdentifier)
	}
	return nil
}

func (s *enableSession) Close() {
	if s == nil || s.client == nil {
		return
	}
	if err := s.client.Close(); err != nil {
		s.modem.Logger().Warn("failed to close LPA client", "error", err)
	}
	s.client = nil
}

func (l *lifecycle) Delete(modem *mmodem.Modem, iccid sgp22.ICCID) error {
	currentSettings := l.settingsSnapshot()
	client, err := l.newClient(modem, currentSettings)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			modem.Logger().Warn("failed to close LPA client", "error", cerr)
		}
	}()

	profiles, err := client.ListProfile(nil, nil)
	if err != nil {
		return fmt.Errorf("list profiles: %w", err)
	}
	if activeProfile(profiles, iccid) {
		return errActiveProfileCannotDelete
	}

	if err := client.Delete(iccid); err != nil {
		return fmt.Errorf("delete profile %s: %w", iccid.String(), err)
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
	currentSettings := l.settingsSnapshot()
	client, err := l.newClient(modem, currentSettings)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			modem.Logger().Warn("failed to close LPA client", "error", cerr)
		}
	}()
	notifications, err := client.ListNotification()
	if err != nil {
		return fmt.Errorf("list notifications: %w", err)
	}
	var errs error
	for _, notification := range notifications {
		if notification.SequenceNumber <= lastSeq {
			continue
		}
		if err := client.SendNotification(notification.SequenceNumber, true); err != nil {
			errs = errors.Join(errs, fmt.Errorf("send notification %d: %w", notification.SequenceNumber, err))
		}
	}
	return errs
}
