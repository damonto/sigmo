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
	simSettleDelay             = 100 * time.Millisecond
	simVisiblePollInterval     = time.Second
	simNotReadyRetryInterval   = time.Second
	simNotReadyRetryCount      = 3
	simPostRepowerPollInterval = time.Second
	simPostRepowerPollCount    = 10
)

type SIMTarget struct {
	Slot  uint32
	ICCID string
}

type qmiTargetSIMState struct {
	supported bool
	matches   bool
	ready     bool
	iccid     string
}

func (t SIMTarget) valid() bool {
	return t.Slot != 0 || strings.TrimSpace(t.ICCID) != ""
}

func (m *Registry) EnsureSIMVisible(ctx context.Context, current *Modem, target SIMTarget) (*Modem, error) {
	if current == nil {
		return nil, errModemRequired
	}
	if !target.valid() {
		return nil, errors.New("SIM target is required")
	}

	ticker := time.NewTicker(simVisiblePollInterval)
	defer ticker.Stop()

	var refreshedModemManager bool
	var reloadedModemManager bool
	var activatedProvisioning bool
	var repoweredSIM bool
	var notReadyRetryCount int

	for {
		modem, visible, err := m.readCurrentModem(ctx, current, target)
		if errors.Is(err, ErrNotFound) {
			if err := sleepContext(ctx, simVisiblePollInterval); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			slog.Warn("read modem while waiting for SIM", "imei", current.EquipmentIdentifier, "error", err)
		}
		if visible {
			return modem, nil
		}
		current = modem

		if err := sleepContext(ctx, simSettleDelay); err != nil {
			return nil, err
		}

		modem, visible, err = m.readCurrentModem(ctx, current, target)
		if errors.Is(err, ErrNotFound) {
			if err := sleepContext(ctx, simVisiblePollInterval); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			slog.Warn("read modem after SIM settle delay", "imei", current.EquipmentIdentifier, "error", err)
		}
		if visible {
			return modem, nil
		}
		current = modem

		state, qmiErr := qmiSIMStateForTarget(ctx, current, target)
		if qmiErr != nil {
			slog.Warn("read QMI SIM state", "imei", current.EquipmentIdentifier, "error", qmiErr)
		}
		if qmiErr != nil || !state.supported || !state.matches || state.ready {
			notReadyRetryCount = 0
		}

		switch {
		case qmiErr != nil && !refreshedModemManager:
			refreshedModemManager = true
			if err := current.refreshModemManager(ctx); err != nil {
				return nil, fmt.Errorf("refresh ModemManager SIM state: %w", err)
			}
			continue
		case qmiErr != nil && !reloadedModemManager:
			reloadedModemManager = true
			if err := current.reloadModemManager(ctx); err != nil {
				return nil, fmt.Errorf("reload ModemManager SIM state: %w", err)
			}
			continue
		case qmiErr != nil:
			// QMI returned a partial or unreliable state; do not use it for recovery actions.
		case state.supported && state.matches && state.ready && !refreshedModemManager:
			notReadyRetryCount = 0
			refreshedModemManager = true
			if err := current.refreshModemManager(ctx); err != nil {
				return nil, fmt.Errorf("refresh ModemManager SIM state: %w", err)
			}
			continue
		case state.supported && state.matches && state.ready && !reloadedModemManager:
			notReadyRetryCount = 0
			reloadedModemManager = true
			if err := current.reloadModemManager(ctx); err != nil {
				return nil, fmt.Errorf("reload ModemManager SIM state: %w", err)
			}
			continue
		case state.supported && state.matches && !state.ready && !activatedProvisioning:
			activatedProvisioning = true
			notReadyRetryCount = 0
			if err := qmiActivateProvisioningIfSimMissing(ctx, current); err != nil {
				slog.Warn("activate QMI provisioning session", "imei", current.EquipmentIdentifier, "error", err)
			}
			if err := sleepContext(ctx, simNotReadyRetryInterval); err != nil {
				return nil, err
			}
			continue
		case state.supported && state.matches && !state.ready && !repoweredSIM:
			notReadyRetryCount++
			if notReadyRetryCount < simNotReadyRetryCount {
				if err := sleepContext(ctx, simNotReadyRetryInterval); err != nil {
					return nil, err
				}
				continue
			}
			repoweredSIM = true
			if err := qmiRepowerSimCard(ctx, current); err != nil {
				return nil, fmt.Errorf("repower QMI SIM: %w", err)
			}
			postRepowerCtx, cancel := context.WithTimeout(context.Background(), simPostRepowerTimeout())
			modem, visible, err := m.waitForSIMVisibleInModemManager(postRepowerCtx, current, target)
			cancel()
			if err != nil {
				slog.Warn("read modem after SIM repower", "imei", current.EquipmentIdentifier, "error", err)
			}
			if visible {
				return modem, nil
			}
			current = modem
			continue
		case !state.supported && !refreshedModemManager:
			refreshedModemManager = true
			if err := current.refreshModemManager(ctx); err != nil {
				return nil, fmt.Errorf("refresh ModemManager SIM state: %w", err)
			}
			continue
		case !state.supported && !reloadedModemManager:
			reloadedModemManager = true
			if err := current.reloadModemManager(ctx); err != nil {
				return nil, fmt.Errorf("reload ModemManager SIM state: %w", err)
			}
			continue
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func simPostRepowerTimeout() time.Duration {
	if simPostRepowerPollCount <= 0 {
		return 5 * time.Second
	}
	return time.Duration(simPostRepowerPollCount)*simPostRepowerPollInterval + 5*time.Second
}

func (m *Registry) waitForSIMVisibleInModemManager(ctx context.Context, current *Modem, target SIMTarget) (*Modem, bool, error) {
	var lastErr error
	for range simPostRepowerPollCount {
		if err := sleepContext(ctx, simPostRepowerPollInterval); err != nil {
			return current, false, err
		}
		modem, visible, err := m.readCurrentModem(ctx, current, target)
		if err != nil {
			lastErr = err
			continue
		}
		if visible {
			return modem, true, nil
		}
		current = modem
	}
	return current, false, lastErr
}

func (m *Registry) readCurrentModem(ctx context.Context, current *Modem, target SIMTarget) (*Modem, bool, error) {
	modem, err := m.findModem(ctx, current.EquipmentIdentifier)
	if err != nil {
		return current, false, err
	}
	return modem, modemMatchesSIMTarget(modem, target), nil
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
	if m == nil {
		return false
	}
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
	if m == nil || m.PrimaryPortType() != ModemPortTypeQmi {
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

	state := qmiTargetSIMState{supported: true}
	slotStatus, err := reader.SlotStatus(ctx)
	if err != nil && !errors.Is(err, qcom.QMIErrorNotSupported) {
		return state, fmt.Errorf("read QMI UIM slot status: %w", err)
	}
	if err == nil {
		iccid, err := qmiICCIDForSlot(slotStatus, slot)
		if err != nil {
			return state, err
		}
		state.iccid = iccid
		state.matches = qmiSlotMatchesTarget(slotStatus, slot, state.iccid, target)
	}

	cardStatus, err := reader.CardStatus(ctx)
	if err != nil {
		return state, fmt.Errorf("read QMI UIM card status: %w", err)
	}
	state.ready = qmiUSIMReadyForSlot(cardStatus, slot)
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
	iccid, err := decodeQMIICCID(status.Slots[slot-1].ICCID)
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
