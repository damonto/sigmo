//go:build ims

package ims

import (
	"context"
	"errors"
	"fmt"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

const airplaneModeVoLTERestoreTimeout = 30 * time.Second

type airplaneModeTransition struct {
	targetEnabled     bool
	radioStateApplied bool
}

func (t airplaneModeTransition) shouldResumeConnectivity() bool {
	// Connectivity resumes when enabling was rejected or disabling was applied.
	return t.targetEnabled != t.radioStateApplied
}

func (t airplaneModeTransition) shouldReloadQMI(modem *mmodem.Modem) bool {
	return !t.targetEnabled && t.radioStateApplied && modem.PrimaryPortType() == wwanmodem.PortQMI
}

func (c *Connectivity) VoLTEStatus(ctx context.Context, modem *mmodem.Modem) (VoLTEStatus, error) {
	if c == nil || c.volte == nil {
		return VoLTEStatus{}, ErrUnavailable
	}
	settings, err := c.volte.VoLTESettings(ctx, modem)
	if err != nil {
		return VoLTEStatus{}, err
	}
	status, err := c.volte.SessionStatus(ctx, modem, settings.Enabled)
	if err != nil {
		return VoLTEStatus{}, err
	}
	return VoLTEStatus{
		VoLTESettings:   settings,
		Connected:       status.Connected,
		State:           status.State,
		DurationSeconds: status.DurationSeconds,
	}, nil
}

func (c *Connectivity) ReplaceVoLTESettings(ctx context.Context, modem *mmodem.Modem, settings VoLTESettings) error {
	if c == nil || c.volte == nil || modem == nil {
		return ErrUnavailable
	}
	settings, err := ResolveVoLTESettings(modem, settings)
	if err != nil {
		return err
	}
	return c.change(modem, func() error {
		airplaneMode, err := c.volte.airplaneModeEnabled(ctx, modem)
		if err != nil {
			return err
		}
		if airplaneMode {
			return ErrVoLTEAirplaneMode
		}
		return c.volte.managedVoLTEOperations().updateResolvedSettings(ctx, modem, c.volte, settings)
	})
}

func (c *Connectivity) ChangeAirplaneMode(ctx context.Context, modem *mmodem.Modem, targetEnabled bool, apply func() (applied bool, err error)) error {
	if c == nil || c.volte == nil || modem == nil || apply == nil {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.change(modem, func() error {
		stateErr := c.prepareVoLTEAirplaneModeChange(ctx, modem, targetEnabled)
		beginInternetErr := c.beginInternetAirplaneModeChange(ctx, modem, targetEnabled)
		applied, changeErr := apply()
		transition := airplaneModeTransition{
			targetEnabled:     targetEnabled,
			radioStateApplied: applied,
		}
		if !transition.shouldResumeConnectivity() {
			completeInternetErr := c.completeInternetAirplaneModeChange(modem, transition)
			return errors.Join(stateErr, beginInternetErr, changeErr, completeInternetErr)
		}

		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), airplaneModeVoLTERestoreTimeout)
		defer cancel()
		restoreModem, reloadErr := c.reloadAfterAirplaneMode(restoreCtx, modem, transition)
		if reloadErr != nil {
			internetErr := c.completeInternetAirplaneModeChange(modem, transition)
			c.volte.endAirplaneModeChange(ctx, modem.EquipmentIdentifier)
			return errors.Join(stateErr, beginInternetErr, changeErr, reloadErr, internetErr)
		}
		resumeErr := c.resumeAfterAirplaneMode(restoreCtx, restoreModem, transition)
		if resumeErr != nil {
			if targetEnabled {
				resumeErr = fmt.Errorf("resume connectivity after airplane mode rollback: %w", resumeErr)
			} else {
				resumeErr = fmt.Errorf("resume connectivity after airplane mode: %w", resumeErr)
			}
		}
		return errors.Join(stateErr, beginInternetErr, changeErr, resumeErr)
	})
}

func (c *Connectivity) prepareVoLTEAirplaneModeChange(ctx context.Context, modem *mmodem.Modem, targetEnabled bool) error {
	wasAirplaneMode, err := c.volte.airplaneModeEnabled(ctx, modem)
	c.volte.beginAirplaneModeChange(modem.EquipmentIdentifier, targetEnabled || wasAirplaneMode || err != nil)
	if targetEnabled {
		c.volte.stop(ctx, modem.EquipmentIdentifier)
	}
	if err != nil {
		return fmt.Errorf("read airplane mode before VoLTE transition: %w", err)
	}
	return nil
}

func (c *Connectivity) beginInternetAirplaneModeChange(ctx context.Context, modem *mmodem.Modem, targetEnabled bool) error {
	if c.internet == nil {
		return nil
	}
	if err := c.internet.BeginAirplaneModeChange(ctx, modem, targetEnabled); err != nil {
		return fmt.Errorf("begin Internet airplane mode transition: %w", err)
	}
	return nil
}

func (c *Connectivity) completeInternetAirplaneModeChange(modem *mmodem.Modem, transition airplaneModeTransition) error {
	if c.internet == nil {
		return nil
	}
	if err := c.internet.CompleteAirplaneModeChange(
		modem,
		transition.targetEnabled,
		transition.radioStateApplied,
	); err != nil {
		return fmt.Errorf("restore Internet policy: %w", err)
	}
	return nil
}

func (c *Connectivity) reloadAfterAirplaneMode(ctx context.Context, modem *mmodem.Modem, transition airplaneModeTransition) (*mmodem.Modem, error) {
	if !transition.shouldReloadQMI(modem) || c.reloadModem == nil {
		return modem, nil
	}
	replacement, err := c.reloadModem(ctx, modem)
	if err != nil {
		return modem, fmt.Errorf("reload QMI modem after airplane mode: %w", err)
	}
	if replacement == nil {
		return modem, errors.New("reload QMI modem after airplane mode: replacement is nil")
	}
	return replacement, nil
}

func (c *Connectivity) resumeAfterAirplaneMode(ctx context.Context, modem *mmodem.Modem, transition airplaneModeTransition) error {
	settings, err := c.volte.VoLTESettings(ctx, modem)
	if err != nil {
		err = fmt.Errorf("read VoLTE settings: %w", err)
	}
	var (
		configureErr error
		profileID    string
	)
	if err == nil && settings.Enabled {
		configureErr = c.volte.configureVoLTEDataPath(ctx, modem, settings.DataPath)
		if configureErr != nil {
			configureErr = fmt.Errorf("configure VoLTE data path: %w", configureErr)
		} else {
			profileID, configureErr = modem.ProfileID(ctx)
			if configureErr != nil {
				configureErr = fmt.Errorf("read VoLTE profile: %w", configureErr)
			}
		}
	}
	internetErr := c.completeInternetAirplaneModeChange(modem, transition)
	c.volte.endAirplaneModeChange(ctx, modem.EquipmentIdentifier)
	if err == nil && configureErr == nil && settings.Enabled {
		c.volte.start(ctx, modem, profileID)
	}
	return errors.Join(err, configureErr, internetErr)
}
