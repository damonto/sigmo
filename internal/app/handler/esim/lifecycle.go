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

func newLifecycle(cfg *config.Config, manager *mmodem.Manager) *lifecycle {
	return &lifecycle{
		cfg:       cfg,
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

func (l *lifecycle) PrepareEnable(modem *mmodem.Modem, iccid sgp22.ICCID) (*enableSession, error) {
	client, err := l.newClient(modem, l.cfg)
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
		return s.confirmEnableResult(ctx, err)
	}

	s.Close()

	if err := s.l.restartModem(s.modem, s.l.cfg.FindModem(s.modem.EquipmentIdentifier).Compatible); err != nil {
		slog.Warn("restart modem after enabling profile", "modem", s.modem.EquipmentIdentifier, "error", err)
		return s.confirmEnableResult(ctx, err)
	}

	target, state, err := s.l.waitForEnabledProfile(ctx, s.modem.EquipmentIdentifier, s.iccid)
	if err != nil {
		slog.Error(
			"confirm enabled profile",
			"modem", s.modem.EquipmentIdentifier,
			"error", err,
			"last_confirm_failure", state.lastConfirmFailure,
		)
		return err
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
			slog.Error(
				"timed out waiting for modem to return",
				"modem", s.modem.EquipmentIdentifier,
				"error", err,
				"reload_observed", state.reloadObserved,
				"last_confirm_failure", state.lastConfirmFailure,
			)
			return err
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
	client, err := l.newClient(modem, l.cfg)
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

func (l *lifecycle) profileEnabled(modem *mmodem.Modem, iccid sgp22.ICCID) (bool, error) {
	client, err := l.newClient(modem, l.cfg)
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
	client, err := l.newClient(modem, l.cfg)
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
