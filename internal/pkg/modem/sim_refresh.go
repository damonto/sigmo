package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

var (
	simSettleDelay              = 100 * time.Millisecond
	simVisiblePollInterval      = time.Second
	simReenumerationGracePeriod = time.Second
)

type SIMTarget struct {
	Slot  uint32
	ICCID string
}

type simRefreshResult struct {
	Modem          *Modem
	ReloadObserved bool
}

type currentModemRead struct {
	Modem          *Modem
	SIMVisible     bool
	ReloadObserved bool
}

func (t SIMTarget) valid() bool {
	return t.Slot != 0 || strings.TrimSpace(t.ICCID) != ""
}

func (m *Registry) EnsureSIMVisible(ctx context.Context, current *Modem, target SIMTarget) (*Modem, error) {
	result, err := m.ensureSIMVisible(ctx, current, target, true, false)
	if err != nil {
		return nil, err
	}
	return result.Modem, nil
}

func (m *Registry) PowerCycleSIM(ctx context.Context, current *Modem, target SIMTarget) (*Modem, error) {
	result, err := m.powerCycleSIM(ctx, current, target)
	if err != nil {
		return nil, err
	}
	return result.Modem, nil
}

func (m *Registry) PowerCycleSIMAndWait(ctx context.Context, current *Modem, target SIMTarget) (*Modem, error) {
	return m.waitForModemAfterAction(ctx, current, false, func() (modemWaitActionResult, error) {
		result, err := m.powerCycleSIM(ctx, current, target)
		if err != nil {
			return modemWaitActionResult{}, err
		}
		return modemWaitActionResult{ReloadObserved: result.ReloadObserved}, nil
	})
}

func (m *Registry) powerCycleSIM(ctx context.Context, current *Modem, target SIMTarget) (simRefreshResult, error) {
	if current == nil {
		return simRefreshResult{}, errModemRequired
	}
	target = currentSIMTarget(current, target)
	if !target.valid() {
		return simRefreshResult{}, errors.New("SIM target is required")
	}
	device, err := openQMIDeviceForTarget(current, target, m.deviceOpener())
	if err != nil {
		return simRefreshResult{}, err
	}
	if err := device.PowerCycleSIM(ctx); err != nil {
		return simRefreshResult{}, fmt.Errorf("power cycle SIM: %w", err)
	}
	return m.ensureSIMVisible(ctx, current, target, false, true)
}

func currentSIMTarget(current *Modem, target SIMTarget) SIMTarget {
	if target.valid() {
		return target
	}
	target.Slot = current.PrimarySimSlot
	if current.Sim != nil {
		target.ICCID = strings.TrimSpace(current.Sim.Identifier)
	}
	if target.valid() {
		return target
	}
	switch current.PrimaryPortType() {
	case ModemPortTypeQmi, ModemPortTypeMbim:
		slot, err := deviceSlot(current)
		if err != nil {
			return target
		}
		target.Slot = uint32(slot)
	}
	return target
}

func (m *Registry) ensureSIMVisible(ctx context.Context, current *Modem, target SIMTarget, allowPowerCycleFallback bool, initialPowerCycled bool) (simRefreshResult, error) {
	if current == nil {
		return simRefreshResult{}, errModemRequired
	}
	if !target.valid() {
		return simRefreshResult{}, errors.New("SIM target is required")
	}

	ticker := time.NewTicker(simVisiblePollInterval)
	defer ticker.Stop()

	var refreshedModemManager bool
	var reloadedModemManager bool
	var activatedProvisioning bool
	powerCycledSIM := initialPowerCycled
	var reenumerated bool
	var unchangedModemSince time.Time

	for {
		read, err := m.readCurrentModem(ctx, current, target)
		if read.ReloadObserved {
			reenumerated = true
		}
		if errors.Is(err, ErrNotFound) {
			reenumerated = true
			if err := sleepContext(ctx, simVisiblePollInterval); err != nil {
				return simRefreshResult{}, err
			}
			continue
		}
		if err != nil {
			slog.Warn("read modem while waiting for SIM", "imei", current.EquipmentIdentifier, "error", err)
		}
		if read.SIMVisible {
			return simRefreshResult{Modem: read.Modem, ReloadObserved: reenumerated}, nil
		}
		current = read.Modem

		if err := sleepContext(ctx, simSettleDelay); err != nil {
			return simRefreshResult{}, err
		}

		read, err = m.readCurrentModem(ctx, current, target)
		if read.ReloadObserved {
			reenumerated = true
		}
		if errors.Is(err, ErrNotFound) {
			reenumerated = true
			if err := sleepContext(ctx, simVisiblePollInterval); err != nil {
				return simRefreshResult{}, err
			}
			continue
		}
		if err != nil {
			slog.Warn("read modem after SIM settle delay", "imei", current.EquipmentIdentifier, "error", err)
		}
		if read.SIMVisible {
			return simRefreshResult{Modem: read.Modem, ReloadObserved: reenumerated}, nil
		}
		current = read.Modem

		if reenumerated || powerCycledSIM {
			state, stateErr := readDeviceSIMState(ctx, current, target, m.deviceOpener())
			if stateErr != nil {
				slog.Warn("read device SIM state", "imei", current.EquipmentIdentifier, "error", stateErr)
			}

			switch {
			case stateErr != nil:
				// Partial or unreliable device state must not drive recovery actions.
				refreshed, err := refreshModemManagerSIMStateInOrder(ctx, current, &refreshedModemManager, &reloadedModemManager)
				if err != nil {
					return simRefreshResult{}, err
				}
				if refreshed {
					continue
				}
			case state.Supported && state.Recoverable && !state.Ready && !state.ICCIDMismatch && !activatedProvisioning:
				activatedProvisioning = true
				device, err := openQMIDeviceForSlot(current, state.Slot, m.deviceOpener())
				if err != nil {
					slog.Warn("open device for provisioning session", "imei", current.EquipmentIdentifier, "error", err)
				} else if err := device.ActivateProvisioningIfSIMMissing(ctx); err != nil {
					slog.Warn("activate device provisioning session", "imei", current.EquipmentIdentifier, "error", err)
				}
				refreshed, err := refreshModemManagerSIMStateInOrder(ctx, current, &refreshedModemManager, &reloadedModemManager)
				if err != nil {
					return simRefreshResult{}, err
				}
				if refreshed {
					continue
				}
			case state.Supported && state.Recoverable && !state.Ready && !state.ICCIDMismatch:
				refreshed, err := refreshModemManagerSIMStateInOrder(ctx, current, &refreshedModemManager, &reloadedModemManager)
				if err != nil {
					return simRefreshResult{}, err
				}
				if refreshed {
					continue
				}
			case state.Supported && state.Recoverable && state.Ready:
				refreshed, err := refreshModemManagerSIMStateInOrder(ctx, current, &refreshedModemManager, &reloadedModemManager)
				if err != nil {
					return simRefreshResult{}, err
				}
				if refreshed {
					continue
				}
			case !state.Supported:
				refreshed, err := refreshModemManagerSIMStateInOrder(ctx, current, &refreshedModemManager, &reloadedModemManager)
				if err != nil {
					return simRefreshResult{}, err
				}
				if refreshed {
					continue
				}
			}
		}

		if allowPowerCycleFallback && !reenumerated && !powerCycledSIM {
			if unchangedModemSince.IsZero() {
				unchangedModemSince = time.Now()
			}
			if time.Since(unchangedModemSince) < simReenumerationGracePeriod {
				select {
				case <-ctx.Done():
					return simRefreshResult{}, ctx.Err()
				case <-ticker.C:
				}
				continue
			}
			device, err := openQMIDeviceForTarget(current, target, m.deviceOpener())
			if err != nil {
				return simRefreshResult{}, err
			}
			powerCycledSIM = true
			if err := device.PowerCycleSIM(ctx); err != nil {
				return simRefreshResult{}, fmt.Errorf("power cycle SIM: %w", err)
			}
			continue
		}

		select {
		case <-ctx.Done():
			return simRefreshResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Registry) deviceOpener() deviceControlOpener {
	if m == nil || m.openDevice == nil {
		return nil
	}
	return m.openDevice
}

func refreshModemManagerSIMStateInOrder(ctx context.Context, current *Modem, refreshedModemManager *bool, reloadedModemManager *bool) (bool, error) {
	if !*refreshedModemManager {
		*refreshedModemManager = true
		if err := refreshModemManagerSIMState(ctx, current); err != nil {
			return false, err
		}
		return true, nil
	}
	if !*reloadedModemManager {
		*reloadedModemManager = true
		if err := reloadModemManagerSIMState(ctx, current); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func refreshModemManagerSIMState(ctx context.Context, current *Modem) error {
	if err := current.RefreshModemManager(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if isRecoverableSIMStateRefreshError(err) {
			slog.Warn("ignore recoverable ModemManager SIM refresh error", "imei", current.EquipmentIdentifier, "error", err)
			return nil
		}
		return fmt.Errorf("refresh ModemManager SIM state: %w", err)
	}
	return nil
}

func reloadModemManagerSIMState(ctx context.Context, current *Modem) error {
	if err := current.reloadModemManager(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if isRecoverableSIMStateRefreshError(err) {
			slog.Warn("ignore recoverable ModemManager SIM reload error", "imei", current.EquipmentIdentifier, "error", err)
			return nil
		}
		return fmt.Errorf("reload ModemManager SIM state: %w", err)
	}
	return nil
}

func isRecoverableSIMStateRefreshError(err error) bool {
	if err == nil {
		return false
	}
	return isTransientRestartError(err) || isAbortedError(err)
}

func (m *Registry) readCurrentModem(ctx context.Context, current *Modem, target SIMTarget) (currentModemRead, error) {
	modem, err := m.findModem(ctx, current.EquipmentIdentifier)
	if err != nil {
		return currentModemRead{Modem: current}, err
	}
	return currentModemRead{
		Modem:          modem,
		SIMVisible:     modemMatchesSIMTarget(modem, target),
		ReloadObserved: modemReenumerated(current, modem),
	}, nil
}

func modemReenumerated(current, next *Modem) bool {
	if current.objectPath != "" && next.objectPath != "" && current.objectPath != next.objectPath {
		return true
	}
	return false
}

func (m *Registry) findModem(ctx context.Context, id string) (*Modem, error) {
	if m.dbusObject != nil {
		return m.Find(ctx, id)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, modem := range m.modems {
		if strings.TrimSpace(modem.EquipmentIdentifier) == id {
			return modem, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

func modemMatchesSIMTarget(m *Modem, target SIMTarget) bool {
	if target.Slot != 0 && m.PrimarySimSlot != target.Slot {
		return false
	}
	if target.ICCID != "" {
		if m.Sim == nil || strings.TrimSpace(m.Sim.Identifier) != strings.TrimSpace(target.ICCID) {
			return false
		}
	}
	return true
}
