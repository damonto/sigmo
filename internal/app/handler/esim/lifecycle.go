package esim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

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
	l             *lifecycle
	modem         *mmodem.Modem
	seID          string
	iccid         sgp22.ICCID
	previousICCID string
	client        lifecycleClient
	lastSeq       sgp22.SequenceNumber
}

type lifecycleClient interface {
	ListProfile(any, []bertlv.Tag) ([]*sgp22.ProfileInfo, error)
	ListNotification(...sgp22.NotificationEvent) ([]*sgp22.NotificationMetadata, error)
	RetrieveNotificationList(any) ([]*sgp22.PendingNotification, error)
	HandleNotification(*sgp22.PendingNotification) error
	EnableProfile(any, bool) error
	Delete(sgp22.ICCID) error
	RemoveNotificationFromList(sgp22.SequenceNumber) error
	Invalidate() error
	Close() error
}

type lifecycleClientFactory func(context.Context, *mmodem.Modem, *settings.Settings, string) (lifecycleClient, error)

var (
	errActiveProfileCannotDelete = errors.New("active profile cannot be deleted")
	errProfileNotFound           = errors.New("profile not found")
	errProfileAlreadyActive      = errors.New("profile already active")
)

func newLifecycle(store *settings.Store, registry *mmodem.Registry, clients *lpa.Pool) *lifecycle {
	return &lifecycle{
		store:            store,
		newClient:        newLifecycleClient(clients),
		ensureSIMVisible: registry.EnsureSIMVisible,
	}
}

func newLifecycleClient(clients *lpa.Pool) lifecycleClientFactory {
	return func(ctx context.Context, modem *mmodem.Modem, _ *settings.Settings, seID string) (lifecycleClient, error) {
		return clients.Acquire(ctx, modem, seID)
	}
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

func (l *lifecycle) PrepareEnable(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID) (*enableSession, error) {
	currentSettings := l.settingsSnapshot()
	client, err := l.newClient(ctx, modem, currentSettings, seID)
	if err != nil {
		return nil, fmt.Errorf("create LPA client: %w", err)
	}
	session := &enableSession{
		l:             l,
		modem:         modem,
		seID:          seID,
		iccid:         iccid,
		previousICCID: activeModemICCID(modem),
		client:        client,
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

	if err := s.invalidateClient(); err != nil {
		slog.Warn("invalidate LPA client after profile enable", "imei", s.modem.EquipmentIdentifier, "error", err)
	}
	return s.finish(ctx)
}

func (s *enableSession) finish(ctx context.Context) error {
	target, err := s.l.ensureSIMVisible(ctx, s.modem, mmodem.SIMTarget{
		ICCID:         s.iccid.String(),
		PreviousICCID: s.previousICCID,
		RequireEUICC:  true,
	})
	if err != nil {
		return fmt.Errorf("wait for modem readiness: %w", err)
	}
	if err := s.l.sendPendingNotifications(ctx, target, s.seID, s.lastSeq); err != nil {
		slog.Warn("handle eSIM profile notifications", "error", err, "imei", s.modem.EquipmentIdentifier)
	}
	return nil
}

func activeModemICCID(modem *mmodem.Modem) string {
	if modem == nil {
		return ""
	}
	sim := modem.Snapshot().SIM
	if sim == nil {
		return ""
	}
	return strings.TrimSpace(sim.Identifier)
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

func (s *enableSession) invalidateClient() error {
	if s == nil || s.client == nil {
		return nil
	}
	client := s.client
	s.client = nil
	return client.Invalidate()
}

func (l *lifecycle) Delete(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID) error {
	currentSettings := l.settingsSnapshot()
	client, err := l.newClient(ctx, modem, currentSettings, seID)
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
	profile, ok := profileByICCID(profiles, iccid)
	if !ok {
		return errProfileNotFound
	}
	if profile.ProfileState == sgp22.ProfileEnabled {
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

func (l *lifecycle) sendPendingNotifications(ctx context.Context, modem *mmodem.Modem, seID string, lastSeq sgp22.SequenceNumber) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	currentSettings := l.settingsSnapshot()
	client, err := l.newClient(ctx, modem, currentSettings, seID)
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
		if err := ctx.Err(); err != nil {
			return errors.Join(errs, err)
		}
		if notification.SequenceNumber <= lastSeq {
			continue
		}
		pending, err := client.RetrieveNotificationList(notification.SequenceNumber)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("retrieve notification %d: %w", notification.SequenceNumber, err))
			continue
		}
		if len(pending) == 0 {
			errs = errors.Join(errs, fmt.Errorf("retrieve notification %d: payload not found", notification.SequenceNumber))
			continue
		}
		handled := true
		for _, item := range pending {
			if err := ctx.Err(); err != nil {
				return errors.Join(errs, err)
			}
			if err := client.HandleNotification(item); err != nil {
				errs = errors.Join(errs, fmt.Errorf("send notification %d: %w", notification.SequenceNumber, err))
				handled = false
			}
		}
		if !handled {
			continue
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(errs, err)
		}
		if err := client.RemoveNotificationFromList(notification.SequenceNumber); err != nil {
			errs = errors.Join(errs, fmt.Errorf("remove notification %d: %w", notification.SequenceNumber, err))
		}
	}
	return errs
}
