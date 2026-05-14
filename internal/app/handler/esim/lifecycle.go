package esim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/damonto/euicc-go/bertlv"
	sgp22 "github.com/damonto/euicc-go/v2"

	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type lifecycle struct {
	cfg             *config.Config
	store           *config.Store
	newClient       lifecycleClientFactory
	findModem       func(string) (*mmodem.Modem, error)
	restartModem    func(*mmodem.Modem, bool) error
	confirmInterval time.Duration
}

type enableSession struct {
	l       *lifecycle
	modem   *mmodem.Modem
	iccid   sgp22.ICCID
	client  lifecycleClient
	lastSeq sgp22.SequenceNumber
}

type enableConfirmState struct {
	reloadObserved     bool
	modemUnavailable   bool
	lastConfirmFailure error
}

type lifecycleClient interface {
	ListProfile(any, []bertlv.Tag) ([]*sgp22.ProfileInfo, error)
	ListNotification(...sgp22.NotificationEvent) ([]*sgp22.NotificationMetadata, error)
	EnableProfile(any, bool) error
	Delete(sgp22.ICCID) error
	SendNotification(any, bool) error
	Close() error
}

type lifecycleClientFactory func(*mmodem.Modem, *config.Config) (lifecycleClient, error)

var (
	errActiveProfileCannotDelete = errors.New("active profile cannot be deleted")
	errProfileNotFound           = errors.New("profile not found")
	errProfileAlreadyActive      = errors.New("profile already active")
)

const enableConfirmInterval = time.Second

func newLifecycle(store *config.Store, manager *mmodem.Manager) *lifecycle {
	return &lifecycle{
		store:     store,
		newClient: newLifecycleClient,
		findModem: manager.Find,
		restartModem: func(modem *mmodem.Modem, compatible bool) error {
			return modem.Restart(compatible)
		},
		confirmInterval: enableConfirmInterval,
	}
}

func newLifecycleClient(modem *mmodem.Modem, cfg *config.Config) (lifecycleClient, error) {
	return lpa.New(modem, cfg)
}

func (l *lifecycle) configSnapshot() *config.Config {
	if l.store != nil {
		cfg := l.store.Snapshot()
		return &cfg
	}
	if l.cfg != nil {
		return l.cfg
	}
	return config.Default()
}

func (l *lifecycle) findModemConfig(id string) config.Modem {
	if l.store != nil {
		return l.store.FindModem(id)
	}
	if l.cfg != nil {
		return l.cfg.FindModem(id)
	}
	return config.Default().FindModem(id)
}

func (l *lifecycle) PrepareEnable(modem *mmodem.Modem, iccid sgp22.ICCID) (*enableSession, error) {
	cfg := l.configSnapshot()
	client, err := l.newClient(modem, cfg)
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
		return s.confirmEnableResult(ctx, fmt.Errorf("enable profile %s: %w", s.iccid.String(), err))
	}

	s.Close()

	if err := s.l.restartModem(s.modem, s.l.findModemConfig(s.modem.EquipmentIdentifier).Compatible); err != nil {
		slog.Warn("restart modem after enabling profile", "modem", s.modem.EquipmentIdentifier, "error", err)
		return s.confirmEnableResult(ctx, err)
	}

	target, state, err := s.l.waitForEnabledProfile(ctx, s.modem.EquipmentIdentifier, s.iccid)
	if err != nil {
		return confirmEnabledProfileError(err, state)
	}
	if err := s.l.sendPendingNotifications(target, s.lastSeq); err != nil {
		slog.Warn("failed to handle modem notifications", "error", err, "modem", s.modem.EquipmentIdentifier)
	}
	return nil
}

func (s *enableSession) confirmEnableResult(ctx context.Context, cause error) error {
	s.Close()

	target, state, err := s.l.waitForEnabledProfile(ctx, s.modem.EquipmentIdentifier, s.iccid)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		if state.modemUnavailable && errors.Is(err, context.DeadlineExceeded) {
			return confirmEnabledProfileError(err, state)
		}
		if state.lastConfirmFailure != nil {
			slog.Warn(
				"profile enable confirmation did not succeed",
				"modem", s.modem.EquipmentIdentifier,
				"error", state.lastConfirmFailure,
			)
		}
		return cause
	}

	if err := s.l.sendPendingNotifications(target, s.lastSeq); err != nil {
		slog.Warn("failed to handle modem notifications", "error", err, "modem", s.modem.EquipmentIdentifier)
	}
	return nil
}

func confirmEnabledProfileError(err error, state enableConfirmState) error {
	if state.lastConfirmFailure == nil {
		return fmt.Errorf("confirm enabled profile: %w", err)
	}
	return fmt.Errorf("confirm enabled profile: %w: last confirmation: %v", err, state.lastConfirmFailure)
}

func (l *lifecycle) waitForEnabledProfile(ctx context.Context, modemID string, iccid sgp22.ICCID) (*mmodem.Modem, enableConfirmState, error) {
	interval := l.confirmInterval
	if interval <= 0 {
		interval = enableConfirmInterval
	}
	var state enableConfirmState
	wait := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
			return nil
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil, state, ctx.Err()
		default:
		}

		modem, err := l.findModem(modemID)
		if err != nil {
			if errors.Is(err, mmodem.ErrNotFound) {
				state.reloadObserved = true
				state.modemUnavailable = true
				state.lastConfirmFailure = err
			} else {
				state.modemUnavailable = false
				state.lastConfirmFailure = fmt.Errorf("find modem: %w", err)
			}
			if err := wait(); err != nil {
				return nil, state, err
			}
			continue
		}
		state.modemUnavailable = false

		enabled, err := l.profileEnabled(modem, iccid)
		if err != nil {
			state.lastConfirmFailure = fmt.Errorf("read profile: %w", err)
			if err := wait(); err != nil {
				return nil, state, err
			}
			continue
		}
		if enabled {
			return modem, state, nil
		}
		state.lastConfirmFailure = nil
		if err := wait(); err != nil {
			return nil, state, err
		}
	}
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
	cfg := l.configSnapshot()
	client, err := l.newClient(modem, cfg)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
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

func (l *lifecycle) profileEnabled(modem *mmodem.Modem, iccid sgp22.ICCID) (bool, error) {
	cfg := l.configSnapshot()
	client, err := l.newClient(modem, cfg)
	if err != nil {
		return false, fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Debug("close LPA client after profile check", "error", cerr)
		}
	}()

	profiles, err := client.ListProfile(iccid, nil)
	if err != nil {
		return false, fmt.Errorf("list profiles: %w", err)
	}
	return activeProfile(profiles, iccid), nil
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
	cfg := l.configSnapshot()
	client, err := l.newClient(modem, cfg)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
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
