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
	simSettleDelay         = 100 * time.Millisecond
	simVisiblePollInterval = time.Second
	simProbeTimeout        = 3 * time.Second
)

type SIMTarget struct {
	Slot              uint32
	ICCID             string
	PreviousSlot      uint32
	PreviousICCID     string
	RequireActiveSlot bool
	RequireClassified bool
	RequireEUICC      bool
	AllowLocked       bool
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

type simIdentity struct {
	slot       uint32
	identifier string
}

func (t SIMTarget) valid() bool { return t.Slot != 0 || strings.TrimSpace(t.ICCID) != "" }

// SwitchSIMSlot keeps the active-slot reservation through both the modem
// command and the registry refresh that observes the selected physical card.
func (r *Registry) SwitchSIMSlot(ctx context.Context, current *Modem, slot uint32) (*Modem, error) {
	if current == nil {
		return nil, errModemRequired
	}
	if r == nil {
		return nil, errors.New("modem registry is required")
	}
	if err := current.validatePrimarySIMSlot(slot); err != nil {
		return nil, fmt.Errorf("set primary SIM slot: %w", err)
	}
	var result *Modem
	err := current.withReservedSIMSlot(ctx, func() error {
		snapshot := current.Snapshot()
		previousICCID := ""
		if snapshot.SIM != nil {
			previousICCID = snapshot.SIM.Identifier
		}
		if err := current.setPrimarySIMSlot(ctx, slot); err != nil {
			err = fmt.Errorf("set primary SIM slot: %w", err)
			if !IsTransientRestartError(err) {
				return err
			}
		}
		var err error
		result, err = r.EnsureSIMVisible(ctx, current, SIMTarget{
			Slot:              slot,
			PreviousSlot:      snapshot.PrimarySIMSlot,
			PreviousICCID:     previousICCID,
			RequireActiveSlot: true,
			RequireClassified: true,
		})
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("wait for modem: %w", err)
		}
		return err
	})
	return result, err
}

func (r *Registry) EnsureSIMVisible(ctx context.Context, current *Modem, target SIMTarget) (*Modem, error) {
	if current == nil {
		return nil, errModemRequired
	}
	previousSlot := target.PreviousSlot
	previousSIM := strings.TrimSpace(target.PreviousICCID)
	if previousSlot == 0 || previousSIM == "" {
		snapshot := current.Snapshot()
		if previousSlot == 0 {
			previousSlot = snapshot.PrimarySIMSlot
		}
		if previousSIM == "" && snapshot.SIM != nil {
			previousSIM = strings.TrimSpace(snapshot.SIM.Identifier)
		}
	}
	result, err := r.ensureSIMVisible(ctx, current, target)
	if err != nil {
		return nil, err
	}
	if !result.ReloadObserved {
		r.publishSIMChanged(result.Modem, previousSlot, previousSIM)
	}
	return result.Modem, nil
}

func activeSIMIdentity(m *Modem) (uint32, string) {
	identity := currentSIMIdentity(m)
	return identity.slot, identity.identifier
}

func currentSIMIdentity(m *Modem) simIdentity {
	if m == nil {
		return simIdentity{}
	}
	snapshot := m.Snapshot()
	sim := snapshot.SIM
	if sim == nil {
		return simIdentity{slot: snapshot.PrimarySIMSlot}
	}
	return simIdentity{slot: snapshot.PrimarySIMSlot, identifier: strings.TrimSpace(sim.Identifier)}
}

func (r *Registry) trackSIMIdentity(current *Modem) {
	if r == nil || current == nil {
		return
	}
	r.mu.Lock()
	if r.simIdentities == nil {
		r.simIdentities = make(map[*Modem]simIdentity)
	}
	r.simIdentities[current] = currentSIMIdentity(current)
	r.mu.Unlock()
}

func (r *Registry) publishSIMChanged(current *Modem, previousSlot uint32, previous string) {
	if r == nil || current == nil {
		return
	}
	providedPrevious := simIdentity{slot: previousSlot, identifier: strings.TrimSpace(previous)}
	next := currentSIMIdentity(current)

	r.mu.Lock()
	path := r.keyForModemLocked(current)
	if path == "" {
		r.mu.Unlock()
		return
	}
	if r.simIdentities == nil {
		r.simIdentities = make(map[*Modem]simIdentity)
	}
	trackedPrevious, tracked := r.simIdentities[current]
	if tracked {
		providedPrevious = trackedPrevious
	}
	if providedPrevious == next {
		r.simIdentities[current] = next
		r.mu.Unlock()
		return
	}
	r.simIdentities[current] = next
	current.markNetworkStateChanged()
	snapshot := r.copyModemsLocked()
	subscribers := append([]subscription(nil), r.subs...)
	r.mu.Unlock()
	slog.Info(
		"SIM profile changed",
		"imei", current.EquipmentIdentifier,
		"generation", current.Generation(),
		"previous_slot", providedPrevious.slot,
		"slot", next.slot,
		"previous_iccid", providedPrevious.identifier,
		"iccid", next.identifier,
	)
	r.publish(subscribers, ModemEvent{
		Type:                  ModemEventSIMChanged,
		Modem:                 current,
		Path:                  path,
		Generation:            current.Generation(),
		PreviousSIMSlot:       providedPrevious.slot,
		SIMSlot:               next.slot,
		PreviousSIMIdentifier: providedPrevious.identifier,
		SIMIdentifier:         next.identifier,
		Snapshot:              snapshot,
	})
}

func (r *Registry) ensureSIMVisible(ctx context.Context, current *Modem, target SIMTarget) (simRefreshResult, error) {
	if current == nil {
		return simRefreshResult{}, errModemRequired
	}
	if !target.valid() {
		return simRefreshResult{}, errors.New("SIM target is required")
	}
	provisioned := make(map[uint64]bool)
	active := current
	reloadObserved := false
	for {
		read, err := r.readCurrentModemWithReload(ctx, active, target, reloadObserved)
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
			if err := waitForSIMRefresh(ctx, active, simVisiblePollInterval); err != nil {
				return simRefreshResult{}, err
			}
			continue
		}

		probeTimedOut := errors.Is(err, context.DeadlineExceeded)
		if !probeTimedOut {
			if err := sleepContext(ctx, simSettleDelay); err != nil {
				return simRefreshResult{}, err
			}
			read, err = r.readCurrentModemWithReload(ctx, active, target, reloadObserved)
			if read.Modem != nil {
				active = read.Modem
			}
			reloadObserved = reloadObserved || read.ReloadObserved
			if err == nil && read.SIMVisible {
				return simRefreshResult{Modem: active, ReloadObserved: reloadObserved}, nil
			}
			probeTimedOut = errors.Is(err, context.DeadlineExceeded)
		}

		generation := active.Generation()
		if !target.RequireActiveSlot && !probeTimedOut && !provisioned[generation] {
			provisionCtx, cancel := context.WithTimeout(ctx, simProbeTimeout)
			provisionErr := r.activateProvisioningTransport(provisionCtx, active, target)
			cancel()
			if provisionErr == nil || errors.Is(provisionErr, devicewwan.ErrUnsupported) {
				provisioned[generation] = true
			}
			if provisionErr != nil && !errors.Is(provisionErr, devicewwan.ErrUnsupported) {
				slog.Warn("activate modem provisioning while waiting for SIM", "imei", active.EquipmentIdentifier, "generation", generation, "error", provisionErr)
			}
		}
		if err := ctx.Err(); err != nil {
			return simRefreshResult{}, err
		}
		if err := waitForSIMRefresh(ctx, active, simVisiblePollInterval); err != nil {
			return simRefreshResult{}, err
		}
	}
}

func waitForSIMRefresh(ctx context.Context, modem *Modem, timeout time.Duration) error {
	if timeout <= 0 {
		return ctx.Err()
	}
	_, _, refresh := modem.currentSIMRefresh()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-refresh:
		return nil
	case <-timer.C:
		return nil
	}
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
	return r.readCurrentModemWithReload(ctx, current, target, false)
}

func (r *Registry) readCurrentModemWithReload(ctx context.Context, current *Modem, target SIMTarget, reloadObserved bool) (currentModemRead, error) {
	modem, err := r.findModem(current.EquipmentIdentifier)
	if err != nil {
		return currentModemRead{Modem: current, ReloadObserved: true}, err
	}
	reloaded := modem.Generation() != current.Generation() || modem.Path() != current.Path()
	observedReload := reloadObserved || reloaded
	// A PIN-locked profile can hide its ICCID and active slot. Accept that
	// state only after the registry has observed the modem generation created
	// by the profile refresh, never from the generation that issued the switch.
	if activeSIMTargetReady(modem, target) || (observedReload && lockedSIMTargetReady(modem, target)) {
		return r.confirmCurrentReadyModem(currentModemRead{Modem: modem, SIMVisible: true, ReloadObserved: observedReload})
	}
	if target.RequireActiveSlot {
		if shouldRefreshSIMClassification(modem, target) {
			probeCtx, cancel := context.WithTimeout(ctx, simProbeTimeout)
			_, refreshErr := modem.SIMs().Primary(probeCtx)
			cancel()
			if refreshErr != nil {
				return currentModemRead{Modem: modem, ReloadObserved: observedReload}, fmt.Errorf("refresh active SIM metadata: %w", refreshErr)
			}
		}
		return r.confirmCurrentReadyModem(currentModemRead{
			Modem:          modem,
			SIMVisible:     activeSIMTargetReady(modem, target),
			ReloadObserved: observedReload,
		})
	}
	probeCtx, cancel := context.WithTimeout(ctx, simProbeTimeout)
	defer cancel()
	read, err := r.readCurrentSIM(probeCtx, modem, target, reloaded)
	if err != nil {
		read.ReloadObserved = observedReload
		return read, err
	}
	read.ReloadObserved = observedReload
	return r.confirmCurrentReadyModem(read)
}

// confirmCurrentReadyModem prevents an in-flight SIM probe from reporting a
// retired modem generation as ready.
func (r *Registry) confirmCurrentReadyModem(read currentModemRead) (currentModemRead, error) {
	if read.Modem == nil || !read.SIMVisible {
		return read, nil
	}
	current, err := r.findModem(read.Modem.EquipmentIdentifier)
	if err != nil {
		read.SIMVisible = false
		read.ReloadObserved = true
		return read, err
	}
	if current != read.Modem {
		return currentModemRead{Modem: current, ReloadObserved: true}, nil
	}
	return read, nil
}

func activeSIMTargetReady(modem *Modem, target SIMTarget) bool {
	if !activeSIMTargetIdentityReady(modem, target) {
		return false
	}
	kind := modem.Snapshot().SIMKind()
	if target.RequireEUICC {
		return kind == SIMKindEUICC
	}
	return !target.RequireClassified || kind != SIMKindUnknown
}

func activeSIMTargetIdentityReady(modem *Modem, target SIMTarget) bool {
	if modem == nil {
		return false
	}
	snapshot := modem.Snapshot()
	if (target.Slot != 0 && snapshot.PrimarySIMSlot != target.Slot) || snapshot.SIM == nil {
		return false
	}
	if snapshot.Status.SIM != wwanmodem.SIMStateReady &&
		(!target.AllowLocked || snapshot.Status.SIM != wwanmodem.SIMStateLocked) {
		return false
	}
	identifier := strings.TrimSpace(snapshot.SIM.Identifier)
	if identifier == "" {
		return false
	}
	if !devicewwan.ICCIDMatches(identifier, target.ICCID) {
		return false
	}
	return true
}

func lockedSIMTargetReady(modem *Modem, target SIMTarget) bool {
	if modem == nil || !target.AllowLocked || strings.TrimSpace(target.ICCID) == "" {
		return false
	}
	snapshot := modem.Snapshot()
	if snapshot.Status.SIM != wwanmodem.SIMStateLocked || snapshot.SIM == nil ||
		strings.TrimSpace(snapshot.SIM.Identifier) != "" {
		return false
	}
	kind := snapshot.SIMKind()
	if target.RequireEUICC {
		return kind == SIMKindEUICC
	}
	return !target.RequireClassified || kind != SIMKindUnknown
}

func shouldRefreshSIMClassification(modem *Modem, target SIMTarget) bool {
	if modem == nil || modem.core == nil || !targetRequiresClassification(target) {
		return false
	}
	return activeSIMTargetIdentityReady(modem, target) && modem.Snapshot().SIMKind() == SIMKindUnknown
}

func (r *Registry) readCurrentSIM(ctx context.Context, modem *Modem, target SIMTarget, reloaded bool) (currentModemRead, error) {
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
	if state.Ready && state.ICCID != "" {
		modem.applyActiveSIMIdentity(state.Slot, state.ICCID)
		if targetRequiresClassification(target) && modem.Snapshot().SIMKind() == SIMKindUnknown {
			// SIMState only confirms the profile identity. Refresh the full SIM
			// metadata so ATR-based eUICC classification can settle before an
			// operation is reported as complete.
			if _, metadataErr := modem.SIMs().Primary(ctx); metadataErr != nil && ctx.Err() != nil {
				return currentModemRead{Modem: modem, ReloadObserved: reloaded}, ctx.Err()
			}
		}
	}
	visible := state.Matches && state.Ready
	if visible && targetRequiresClassification(target) {
		kind := modem.Snapshot().SIMKind()
		visible = kind != SIMKindUnknown && (!target.RequireEUICC || kind == SIMKindEUICC)
	}
	return currentModemRead{
		Modem:          modem,
		SIMVisible:     visible,
		ReloadObserved: reloaded,
	}, nil
}

func targetRequiresClassification(target SIMTarget) bool {
	return target.RequireClassified || target.RequireEUICC
}
