package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/damonto/uicc-go/qcom"
	"github.com/damonto/uicc-go/qcom/uim"
	"github.com/damonto/uicc-go/usim/simfile"
)

var (
	simSettleDelay              = 100 * time.Millisecond
	simVisiblePollInterval      = time.Second
	simReenumerationGracePeriod = time.Second
)

var errQMIRequiredForSIMPowerCycle = errors.New("QMI modem is required for SIM power cycle")

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

type qmiTargetSIMState struct {
	supported     bool
	matches       bool
	recoverable   bool
	ready         bool
	iccidMismatch bool
	iccid         string
	slot          uint8
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
	if current.PrimaryPortType() != ModemPortTypeQmi {
		return simRefreshResult{}, errQMIRequiredForSIMPowerCycle
	}
	slot, err := qmiTargetSlot(current, target)
	if err != nil {
		return simRefreshResult{}, err
	}
	if err := qmiRepowerSimCard(ctx, current, slot); err != nil {
		return simRefreshResult{}, fmt.Errorf("power cycle QMI SIM: %w", err)
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
	if target.valid() || current.PrimaryPortType() != ModemPortTypeQmi {
		return target
	}
	slot, err := qmiSIMSlot(current)
	if err != nil {
		return target
	}
	target.Slot = uint32(slot)
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
			state, qmiErr := qmiSIMStateForTarget(ctx, current, target)
			if qmiErr != nil {
				slog.Warn("read QMI SIM state", "imei", current.EquipmentIdentifier, "error", qmiErr)
			}

			switch {
			case qmiErr != nil:
				// QMI returned a partial or unreliable state; do not use it for recovery actions.
				refreshed, err := refreshModemManagerSIMStateInOrder(ctx, current, &refreshedModemManager, &reloadedModemManager)
				if err != nil {
					return simRefreshResult{}, err
				}
				if refreshed {
					continue
				}
			case state.supported && state.recoverable && !state.ready && !state.iccidMismatch && !activatedProvisioning:
				activatedProvisioning = true
				if err := qmiActivateProvisioningIfSimMissing(ctx, current, state.slot); err != nil {
					slog.Warn("activate QMI provisioning session", "imei", current.EquipmentIdentifier, "error", err)
				}
				refreshed, err := refreshModemManagerSIMStateInOrder(ctx, current, &refreshedModemManager, &reloadedModemManager)
				if err != nil {
					return simRefreshResult{}, err
				}
				if refreshed {
					continue
				}
			case state.supported && state.recoverable && !state.ready && !state.iccidMismatch:
				refreshed, err := refreshModemManagerSIMStateInOrder(ctx, current, &refreshedModemManager, &reloadedModemManager)
				if err != nil {
					return simRefreshResult{}, err
				}
				if refreshed {
					continue
				}
			case state.supported && state.recoverable && state.ready:
				refreshed, err := refreshModemManagerSIMStateInOrder(ctx, current, &refreshedModemManager, &reloadedModemManager)
				if err != nil {
					return simRefreshResult{}, err
				}
				if refreshed {
					continue
				}
			case !state.supported:
				refreshed, err := refreshModemManagerSIMStateInOrder(ctx, current, &refreshedModemManager, &reloadedModemManager)
				if err != nil {
					return simRefreshResult{}, err
				}
				if refreshed {
					continue
				}
			}
		}

		if allowPowerCycleFallback && !reenumerated && !powerCycledSIM && current.PrimaryPortType() == ModemPortTypeQmi {
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
			slot, err := qmiTargetSlot(current, target)
			if err != nil {
				return simRefreshResult{}, err
			}
			powerCycledSIM = true
			if err := qmiRepowerSimCard(ctx, current, slot); err != nil {
				return simRefreshResult{}, fmt.Errorf("power cycle QMI SIM: %w", err)
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

func qmiSIMStateForTarget(ctx context.Context, m *Modem, target SIMTarget) (qmiTargetSIMState, error) {
	if m.PrimaryPortType() != ModemPortTypeQmi {
		return qmiTargetSIMState{}, nil
	}
	slot, err := qmiTargetSlot(m, target)
	if err != nil {
		return qmiTargetSIMState{supported: true}, err
	}
	reader, err := openQMIUIMReader(ctx, m.PrimaryPort, slot)
	if err != nil {
		return qmiTargetSIMState{supported: true}, fmt.Errorf("open QMI UIM reader: %w", err)
	}
	defer closeQMIUIMReader(reader)

	state := qmiTargetSIMState{supported: true, slot: slot}
	var slotStatus uim.SlotStatus
	var slotStatusRead bool
	slotStatus, err = reader.SlotStatus(ctx)
	if err != nil && !errors.Is(err, qcom.QMIErrorNotSupported) {
		return state, fmt.Errorf("read QMI UIM slot status: %w", err)
	}
	if err == nil {
		slotStatusRead = true
		iccid, err := qmiICCIDForSlot(slotStatus, slot)
		if err != nil {
			return state, err
		}
		state.iccid = iccid
		state.matches = qmiSlotMatchesTarget(slotStatus, slot, state.iccid, target)
		targetICCID := strings.TrimSpace(target.ICCID)
		state.iccidMismatch = targetICCID != "" && state.iccid != "" && state.iccid != targetICCID
	}

	cardStatus, err := reader.CardStatus(ctx)
	if err != nil {
		return state, fmt.Errorf("read QMI UIM card status: %w", err)
	}
	state.ready = qmiUSIMReadyForSlot(cardStatus, slot)
	state.recoverable = state.matches
	if !state.recoverable && qmiUSIMPresentForSlot(cardStatus, slot) {
		slotContradicted := target.Slot == 0 && slotStatusRead && slotStatus.ActiveSlot != 0 && slotStatus.ActiveSlot != slot
		state.recoverable = !slotContradicted
	}
	return state, nil
}

func qmiTargetSlot(m *Modem, target SIMTarget) (uint8, error) {
	if target.Slot != 0 {
		if target.Slot > qmiMaxSIMSlot {
			return 0, fmt.Errorf("QMI SIM slot %d is out of range", target.Slot)
		}
		return uint8(target.Slot), nil
	}
	return qmiSIMSlot(m)
}

func qmiSlotMatchesTarget(status uim.SlotStatus, slot uint8, iccid string, target SIMTarget) bool {
	if target.Slot != 0 && status.ActiveSlot != slot {
		return false
	}
	if target.ICCID != "" && iccid != strings.TrimSpace(target.ICCID) {
		return false
	}
	return true
}

func qmiICCIDForSlot(status uim.SlotStatus, slot uint8) (string, error) {
	if slot == 0 || int(slot) > len(status.Slots) {
		return "", nil
	}
	raw := status.Slots[slot-1].ICCID
	if len(raw) == 0 {
		return "", nil
	}
	iccid, err := decodeQMIICCID(raw)
	if err != nil {
		return "", fmt.Errorf("decode QMI slot %d ICCID: %w", slot, err)
	}
	return iccid, nil
}

func decodeQMIICCID(raw []byte) (string, error) {
	var iccid simfile.ICCID
	if err := iccid.UnmarshalBinary(raw); err != nil {
		return "", err
	}
	return iccid.String(), nil
}
