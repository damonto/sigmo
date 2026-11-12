package esim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/damonto/euicc-go/bertlv"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/wwan-go/qcom"

	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

const (
	profileConfirmAttempts = 3
	notificationAttempts   = 3
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
	eid           []byte
}

type deleteSession struct {
	modem  *mmodem.Modem
	client lifecycleClient
	iccid  sgp22.ICCID
}

type lifecycleClient interface {
	EID() ([]byte, error)
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
	ErrActiveProfileCannotDelete = errors.New("active profile cannot be deleted")
	ErrProfileNotFound           = errors.New("profile not found")
	ErrProfileAlreadyActive      = errors.New("profile already active")
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
		return nil, ErrProfileNotFound
	}
	if profile.ProfileState == sgp22.ProfileEnabled {
		return nil, ErrProfileAlreadyActive
	}
	session.eid, err = client.EID()
	if err != nil {
		return nil, fmt.Errorf("read eUICC identity: %w", err)
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

func (s *enableSession) Enable(ctx context.Context) (*mmodem.Modem, error) {
	defer s.Close()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.modem.InvalidateSIMActivation()
	enableErr := s.client.EnableProfile(s.iccid, true)
	if enableErr != nil && !isProfileTransportError(enableErr) {
		return nil, fmt.Errorf("enable profile %s: %w", s.iccid.String(), enableErr)
	}

	if err := s.invalidateClient(); err != nil {
		slog.Warn("invalidate LPA client after profile enable", "imei", s.modem.EquipmentIdentifier, "error", err)
	}
	active, err := s.waitForEnabled(ctx, s.modem)
	if err != nil {
		if enableErr != nil {
			return nil, errors.Join(fmt.Errorf("enable profile %s: %w", s.iccid.String(), enableErr), err)
		}
		return nil, err
	}
	if enableErr != nil {
		slog.Info("confirmed eSIM profile enable after transport error", "imei", s.modem.EquipmentIdentifier, "error", enableErr)
	}
	return active, nil
}

// These transport errors do not establish whether the card accepted Enable.
// Card-level rejections (including CAT busy and policy errors) remain errors.
func isProfileTransportError(err error) bool {
	return mmodem.IsTransientRestartError(err) || errors.Is(err, lpa.ErrModemRetired) ||
		errors.Is(err, qcom.QMIErrorInternal) || errors.Is(err, qcom.QMIErrorInvalidArgument) ||
		errors.Is(err, qcom.QMIErrorAborted) || errors.Is(err, qcom.QMIErrorInvalidClientID)
}

func (s *enableSession) waitForEnabled(ctx context.Context, target *mmodem.Modem) (*mmodem.Modem, error) {
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var err error
		target, err = s.l.ensureSIMVisible(ctx, target, mmodem.SIMTarget{
			ICCID:         s.iccid.String(),
			PreviousICCID: s.previousICCID,
			RequireEUICC:  true,
			AllowLocked:   true,
		})
		if err != nil {
			return nil, fmt.Errorf("wait for modem readiness: %w", err)
		}
		if err = s.confirmEnabled(ctx, target); err == nil {
			return target, nil
		}
		if !isProfileTransportError(err) || attempt == profileConfirmAttempts {
			return nil, err
		}
		if err := waitForProfileRetry(ctx); err != nil {
			return nil, err
		}
	}
}

func (s *enableSession) notify(ctx context.Context, target *mmodem.Modem, registry modemRegistry) {
	var err error
	for attempt := 1; attempt <= notificationAttempts; attempt++ {
		if ctx.Err() != nil {
			return
		}
		err = s.l.sendPendingNotifications(ctx, target, s.seID, s.lastSeq, s.eid)
		if err == nil {
			return
		}
		if !errors.Is(err, lpa.ErrModemRetired) || attempt == notificationAttempts {
			break
		}
		if err = waitForProfileRetry(ctx); err != nil {
			break
		}
		// Another profile may already be enabled now that coordination is
		// released. Notifications belong to the eUICC, not its active profile.
		target, err = registry.Find(ctx, s.modem.EquipmentIdentifier)
		if err != nil {
			break
		}
	}
	if ctx.Err() == nil {
		slog.Warn("handle eSIM profile notifications", "error", err, "imei", s.modem.EquipmentIdentifier)
	}
}

func waitForProfileRetry(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Second):
		return nil
	}
}

func (s *enableSession) confirmEnabled(ctx context.Context, modem *mmodem.Modem) error {
	client, err := s.l.newClient(ctx, modem, s.l.settingsSnapshot(), s.seID)
	if err != nil {
		return fmt.Errorf("create LPA client to confirm profile enable: %w", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			modem.Logger().Warn("close profile confirmation client", "error", err)
		}
	}()
	eid, err := client.EID()
	if err != nil {
		return fmt.Errorf("confirm eUICC identity: %w", err)
	}
	if !bytes.Equal(eid, s.eid) {
		return errors.New("eUICC changed during profile enable")
	}
	profiles, err := client.ListProfile(s.iccid, nil)
	if err != nil {
		return fmt.Errorf("confirm enabled profile: %w", err)
	}
	if !activeProfile(profiles, s.iccid) {
		return errors.New("target eSIM profile is not enabled")
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

// PrepareDelete validates the target while retaining the card lease for deletion.
func (l *lifecycle) PrepareDelete(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID) (*deleteSession, error) {
	currentSettings := l.settingsSnapshot()
	client, err := l.newClient(ctx, modem, currentSettings, seID)
	if err != nil {
		return nil, fmt.Errorf("create LPA client: %w", err)
	}
	session := &deleteSession{modem: modem, client: client, iccid: iccid}
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
		return nil, ErrProfileNotFound
	}
	if profile.ProfileState == sgp22.ProfileEnabled {
		return nil, ErrActiveProfileCannotDelete
	}
	release = true
	return session, nil
}

func (s *deleteSession) Delete() error {
	if err := s.client.Delete(s.iccid); err != nil {
		return fmt.Errorf("delete profile %s: %w", s.iccid.String(), err)
	}
	return nil
}

func (s *deleteSession) Close() {
	if err := s.client.Close(); err != nil {
		s.modem.Logger().Warn("close LPA client", "error", err)
	}
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

func (l *lifecycle) sendPendingNotifications(ctx context.Context, modem *mmodem.Modem, seID string, lastSeq sgp22.SequenceNumber, expectedEID []byte) error {
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
	eid, err := client.EID()
	if err != nil {
		return fmt.Errorf("read notification eUICC identity: %w", err)
	}
	if !bytes.Equal(eid, expectedEID) {
		return errors.New("eUICC changed before notification delivery")
	}
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
