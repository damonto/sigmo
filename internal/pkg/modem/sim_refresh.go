package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	devicewwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	wwanmodem "github.com/damonto/wwan-go/modem"
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

func (t SIMTarget) valid() bool { return t.Slot != 0 || strings.TrimSpace(t.ICCID) != "" }

func (r *Registry) EnsureSIMVisible(ctx context.Context, current *Modem, target SIMTarget) (*Modem, error) {
	result, err := r.ensureSIMVisible(ctx, current, target, true, false)
	if err != nil {
		return nil, err
	}
	return result.Modem, nil
}

func (r *Registry) PowerCycleSIM(ctx context.Context, current *Modem, target SIMTarget) (*Modem, error) {
	result, err := r.powerCycleSIM(ctx, current, target)
	if err != nil {
		return nil, err
	}
	return result.Modem, nil
}

func (r *Registry) PowerCycleSIMAndWait(ctx context.Context, current *Modem, target SIMTarget) (*Modem, error) {
	return r.PowerCycleSIM(ctx, current, target)
}

func (r *Registry) powerCycleSIM(ctx context.Context, current *Modem, target SIMTarget) (simRefreshResult, error) {
	if current == nil {
		return simRefreshResult{}, errModemRequired
	}
	target = currentSIMTarget(current, target)
	if !target.valid() {
		return simRefreshResult{}, errors.New("SIM target is required")
	}
	if err := r.powerCycleSIMTransport(ctx, current, target); err != nil {
		return simRefreshResult{}, fmt.Errorf("power cycle SIM: %w", err)
	}
	return r.ensureSIMVisible(ctx, current, target, false, true)
}

func currentSIMTarget(current *Modem, target SIMTarget) SIMTarget {
	if target.valid() {
		return target
	}
	snapshot := current.Snapshot()
	target.Slot = snapshot.PrimarySIMSlot
	if snapshot.SIM != nil {
		target.ICCID = strings.TrimSpace(snapshot.SIM.Identifier)
	}
	if target.valid() {
		return target
	}
	if slot, err := ActiveSIMSlot(current); err == nil {
		target.Slot = uint32(slot)
	}
	return target
}

func (r *Registry) ensureSIMVisible(ctx context.Context, current *Modem, target SIMTarget, allowPowerCycleFallback, initialPowerCycled bool) (simRefreshResult, error) {
	if current == nil {
		return simRefreshResult{}, errModemRequired
	}
	if !target.valid() {
		return simRefreshResult{}, errors.New("SIM target is required")
	}
	powerCycled := initialPowerCycled
	provisioned := make(map[uint64]bool)
	started := time.Now()
	active := current
	reloadObserved := false
	for {
		read, err := r.readCurrentModem(ctx, active, target)
		if read.Modem != nil {
			active = read.Modem
		}
		reloadObserved = reloadObserved || read.ReloadObserved
		if err == nil && read.SIMVisible {
			return simRefreshResult{Modem: active, ReloadObserved: reloadObserved}, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			slog.Debug("read modem while waiting for SIM", "imei", current.EquipmentIdentifier, "error", err)
		}
		if errors.Is(err, ErrNotFound) {
			if err := sleepContext(ctx, simVisiblePollInterval); err != nil {
				return simRefreshResult{}, err
			}
			continue
		}

		if err := sleepContext(ctx, simSettleDelay); err != nil {
			return simRefreshResult{}, err
		}
		read, err = r.readCurrentModem(ctx, active, target)
		if read.Modem != nil {
			active = read.Modem
		}
		reloadObserved = reloadObserved || read.ReloadObserved
		if err == nil && read.SIMVisible {
			return simRefreshResult{Modem: active, ReloadObserved: reloadObserved}, nil
		}

		generation := active.Generation()
		if !provisioned[generation] {
			provisioned[generation] = true
			if provisionErr := r.activateProvisioningTransport(ctx, active, target); provisionErr != nil && !errors.Is(provisionErr, devicewwan.ErrUnsupported) {
				slog.Warn("activate modem provisioning while waiting for SIM", "imei", active.EquipmentIdentifier, "generation", generation, "error", provisionErr)
			}
		}
		if allowPowerCycleFallback && !powerCycled && time.Since(started) >= simReenumerationGracePeriod {
			powerErr := r.powerCycleSIMTransport(ctx, active, target)
			if powerErr != nil && !errors.Is(powerErr, devicewwan.ErrUnsupported) {
				return simRefreshResult{}, fmt.Errorf("power cycle SIM: %w", powerErr)
			}
			powerCycled = true
		}
		if err := sleepContext(ctx, simVisiblePollInterval); err != nil {
			return simRefreshResult{}, err
		}
	}
}

func (r *Registry) powerCycleSIMTransport(ctx context.Context, current *Modem, target SIMTarget) error {
	slot, err := deviceTargetSlot(current, target)
	if err != nil {
		return err
	}
	device, err := openQMIDeviceForSlot(current, slot, r.deviceOpener())
	if err != nil {
		return err
	}
	return device.PowerCycleSIM(ctx)
}

func (r *Registry) activateProvisioningTransport(ctx context.Context, current *Modem, target SIMTarget) error {
	slot, err := deviceTargetSlot(current, target)
	if err != nil {
		return err
	}
	device, err := openQMIDeviceForSlot(current, slot, r.deviceOpener())
	if err != nil {
		return err
	}
	return device.ActivateProvisioningIfSIMMissing(ctx)
}

func (r *Registry) deviceOpener() deviceControlOpener {
	if r == nil {
		return nil
	}
	return r.openDevice
}

func (r *Registry) readCurrentModem(ctx context.Context, current *Modem, target SIMTarget) (currentModemRead, error) {
	modem, err := r.findModem(current.EquipmentIdentifier)
	if err != nil {
		return currentModemRead{Modem: current, ReloadObserved: true}, err
	}
	reloaded := modem.Generation() != current.Generation() || modem.Path() != current.Path()
	if strings.TrimSpace(target.ICCID) != "" {
		return r.readCurrentESIM(ctx, modem, target, reloaded)
	}
	if modem.core != nil {
		if info, infoErr := modem.core.SIMInfo(ctx); infoErr == nil {
			modem.applySIMInfo(info)
			return currentModemRead{Modem: modem, SIMVisible: simInfoMatchesTarget(info, target), ReloadObserved: reloaded}, nil
		}
	}
	return currentModemRead{Modem: modem, SIMVisible: modemMatchesSIMTarget(modem, target), ReloadObserved: reloaded}, nil
}

func (r *Registry) readCurrentESIM(ctx context.Context, modem *Modem, target SIMTarget, reloaded bool) (currentModemRead, error) {
	slot, err := deviceTargetSlot(modem, target)
	if err != nil {
		return currentModemRead{Modem: modem, ReloadObserved: reloaded}, err
	}
	device, err := openDeviceForSlot(modem, slot, r.deviceOpener())
	if err != nil {
		return currentModemRead{Modem: modem, ReloadObserved: reloaded}, err
	}
	state, err := device.SIMState(ctx, devicewwan.Target{Slot: target.Slot, ICCID: strings.TrimSpace(target.ICCID)})
	if err != nil {
		return currentModemRead{Modem: modem, ReloadObserved: reloaded}, fmt.Errorf("read SIM state: %w", err)
	}
	if state.ICCID != "" {
		modem.applyActiveSIMIdentity(state.Slot, state.ICCID)
	}
	return currentModemRead{
		Modem:          modem,
		SIMVisible:     state.Matches && state.Ready,
		ReloadObserved: reloaded,
	}, nil
}

func simInfoMatchesTarget(info wwanmodem.SIMInfo, target SIMTarget) bool {
	if target.Slot != 0 && uint32(info.Slot) != target.Slot {
		return false
	}
	return target.ICCID == "" || strings.TrimSpace(info.ICCID) == strings.TrimSpace(target.ICCID)
}

func modemMatchesSIMTarget(m *Modem, target SIMTarget) bool {
	snapshot := m.Snapshot()
	if target.Slot != 0 && snapshot.PrimarySIMSlot != target.Slot {
		return false
	}
	return target.ICCID == "" || (snapshot.SIM != nil && strings.TrimSpace(snapshot.SIM.Identifier) == strings.TrimSpace(target.ICCID))
}
