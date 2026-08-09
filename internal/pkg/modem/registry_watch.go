package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"
)

const registryFailureRecoveryAttempts = 3

func (r *Registry) ensureStarted(ctx context.Context) error {
	r.startMu.Lock()
	defer r.startMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.RLock()
	started, closed := r.started, r.closed
	r.mu.RUnlock()
	if closed {
		return errRegistryClosed
	}
	if started {
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	devices, err := r.discover(ctx)
	if err != nil {
		cancel()
		return fmt.Errorf("discover modems: %w", err)
	}
	present := make(map[string]wwanmodem.Device, len(devices))
	for _, device := range devices {
		key := physicalDeviceKey(device)
		if key != "" {
			present[key] = device
		}
	}
	r.clearAbsentCIDRecoveryStates(present)
	opened := make(map[string]*Modem, len(devices))
	for _, device := range devices {
		key := physicalDeviceKey(device)
		if key == "" {
			continue
		}
		if r.cidRecoveryState(key) == cidRecoverySuspended {
			slog.Warn("skip opening modem while QMI client ID recovery is suspended", "device", controlPortPath(device), "physical_path", key)
			continue
		}
		generation := r.nextGenerationToken()
		modem, err := r.open(ctx, device, generation)
		if err != nil {
			if errors.Is(err, qcom.QMIErrorClientIdsExhausted) {
				r.advanceCIDRecoveryState(key)
			}
			slog.Warn("open discovered modem", "device", controlPortPath(device), "physical_path", key, "error", err)
			continue
		}
		if previous := opened[key]; previous != nil {
			_ = previous.Close()
		}
		opened[key] = modem
	}
	stream, err := r.watchDevices(runCtx)
	if err == nil && stream == nil {
		err = errors.New("modem device watcher returned a nil stream")
	}
	if err != nil {
		cancel()
		for _, modem := range opened {
			_ = modem.Close()
		}
		return fmt.Errorf("watch modem devices: %w", err)
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel()
		for _, modem := range opened {
			_ = modem.Close()
		}
		return errRegistryClosed
	}
	r.modems = opened
	r.started = true
	r.cancel = cancel
	r.mu.Unlock()
	for _, modem := range opened {
		r.watchModem(runCtx, modem)
	}
	r.wg.Add(1)
	go r.watchLoop(runCtx, stream)
	return nil
}

func (r *Registry) watchLoop(ctx context.Context, stream <-chan wwanmodem.Result[wwanmodem.DeviceEvent]) {
	defer r.wg.Done()
	for ctx.Err() == nil {
		if stream != nil {
			err := r.consumeDeviceStream(ctx, stream)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				slog.Error("modem device watcher stopped", "error", err)
			}
		}
		if err := r.reconcile(ctx); err != nil && ctx.Err() == nil {
			slog.Error("reconcile modems after watcher stop", "error", err)
		}
		if err := sleepContext(ctx, registryWatchRetryDelay); err != nil {
			return
		}
		next, err := r.watchDevices(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("restart modem device watcher", "error", err)
			}
			stream = nil
			continue
		}
		if next == nil {
			slog.Error("restart modem device watcher", "error", errors.New("watcher returned a nil stream"))
			stream = nil
			continue
		}
		stream = next
	}
}

func (r *Registry) consumeDeviceStream(ctx context.Context, stream <-chan wwanmodem.Result[wwanmodem.DeviceEvent]) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-stream:
			if !ok {
				return errors.New("modem device stream closed")
			}
			if result.Err != nil {
				return result.Err
			}
			r.applyDeviceEvent(ctx, result.Value)
		case failure := <-r.failures:
			r.handleModemFailure(ctx, failure)
		case request := <-r.reloads:
			modem, err := r.reloadModem(ctx, request.modem)
			request.result <- modemReloadResult{modem: modem, err: err}
		}
	}
}

func (r *Registry) reconcile(ctx context.Context) error {
	devices, err := r.discover(ctx)
	if err != nil {
		return fmt.Errorf("discover modems: %w", err)
	}
	present := make(map[string]wwanmodem.Device, len(devices))
	for _, device := range devices {
		key := physicalDeviceKey(device)
		if key == "" {
			continue
		}
		present[key] = device
	}
	r.clearAbsentCIDRecoveryStates(present)
	for _, device := range devices {
		if physicalDeviceKey(device) == "" {
			continue
		}
		r.applyDeviceEvent(ctx, wwanmodem.DeviceEvent{Type: wwanmodem.DevicePresent, Device: device})
	}

	r.mu.RLock()
	current := r.copyModemsLocked()
	r.mu.RUnlock()
	for key, modem := range current {
		if _, ok := present[key]; ok {
			continue
		}
		if modem != nil && devicePresentInSnapshot(modem.deviceInfo, present) {
			continue
		}
		r.removeModem(key, modem)
	}
	return nil
}

func devicePresentInSnapshot(device wwanmodem.Device, devices map[string]wwanmodem.Device) bool {
	for _, candidate := range devices {
		if sameControlDevice(device, candidate) {
			return true
		}
	}
	return false
}

func (r *Registry) applyDeviceEvent(ctx context.Context, event wwanmodem.DeviceEvent) {
	key := physicalDeviceKey(event.Device)
	if key == "" {
		return
	}
	if event.Type == wwanmodem.DeviceRemoved {
		r.clearCIDRecoveryState(key)
		r.mu.RLock()
		existingKey, existing := r.findByDeviceLocked(event.Device)
		r.mu.RUnlock()
		r.removeModem(existingKey, existing)
		return
	}
	if event.Type == wwanmodem.DeviceAdded {
		r.clearCIDRecoveryState(key)
	}

	r.mu.RLock()
	existingKey, existing := r.findByDeviceLocked(event.Device)
	r.mu.RUnlock()
	if event.Type == wwanmodem.DevicePresent && existing != nil && sameDeviceDescription(existing.deviceInfo, event.Device) {
		return
	}
	if existing == nil && r.cidRecoveryState(key) == cidRecoverySuspended {
		return
	}

	var (
		generation  uint64
		replacement *Modem
	)
	for {
		generation = r.nextGenerationToken()
		openCtx, cancel := context.WithTimeout(ctx, registryOpenTimeout)
		var err error
		replacement, err = r.open(openCtx, event.Device, generation)
		cancel()
		if err == nil {
			break
		}

		slog.Warn("open changed modem", "device", controlPortPath(event.Device), "physical_path", key, "error", err)
		if !errors.Is(err, qcom.QMIErrorClientIdsExhausted) {
			return
		}
		state := r.advanceCIDRecoveryState(key)
		if state == cidRecoverySuspended || ctx.Err() != nil {
			return
		}
		// Do not allocate more client IDs while the current generation still owns its IDs.
		if existing != nil {
			return
		}
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = replacement.Close()
		return
	}
	previousKey, previous := r.findReplacementLocked(key, replacement)
	if previous == nil {
		previousKey, previous = existingKey, existing
	}
	if previousKey != "" && previousKey != key {
		delete(r.modems, previousKey)
	}
	r.modems[key] = replacement
	if previous != nil && previous != replacement {
		delete(r.simIdentities, previous)
	}
	if r.simIdentities == nil {
		r.simIdentities = make(map[*Modem]simIdentity)
	}
	r.simIdentities[replacement] = currentSIMIdentity(replacement)
	snapshot := r.copyModemsLocked()
	subscribers := append([]subscription(nil), r.subs...)
	r.mu.Unlock()

	typeOfEvent := ModemEventAdded
	if previous != nil {
		if samePhysicalModem(previous, replacement) {
			typeOfEvent = ModemEventChanged
		} else {
			r.publish(subscribers, ModemEvent{
				Type: ModemEventRemoved, Modem: previous, Path: previousKey,
				Generation: previous.Generation(), Snapshot: snapshot,
			})
		}
	}
	r.publish(subscribers, ModemEvent{
		Type: typeOfEvent, Modem: replacement, Previous: previous, Path: key,
		PreviousPath: previousKey, Generation: generation, Snapshot: snapshot,
	})
	r.watchModem(ctx, replacement)
	if previous != nil {
		if err := previous.Close(); err != nil {
			slog.Warn("close replaced modem", "path", previousKey, "error", err)
		}
	}
}

func (r *Registry) watchModem(ctx context.Context, modem *Modem) {
	r.trackSIMIdentity(modem)
	modem.startRuntimeWatchers(ctx, func(err error) {
		if r.failures == nil {
			return
		}
		select {
		case r.failures <- modemFailure{modem: modem, err: err}:
		case <-ctx.Done():
		}
	}, func(previousSlot uint32, previousIdentifier string) {
		r.publishSIMChanged(modem, previousSlot, previousIdentifier)
	})
}

func (r *Registry) handleModemFailure(ctx context.Context, failure modemFailure) {
	if failure.modem == nil {
		return
	}
	r.mu.RLock()
	key := r.keyForModemLocked(failure.modem)
	r.mu.RUnlock()
	if key == "" {
		return
	}

	clientIDsExhausted := errors.Is(failure.err, qcom.QMIErrorClientIdsExhausted)
	recoverySuspended := clientIDsExhausted && r.advanceCIDRecoveryState(key) == cidRecoverySuspended
	message := "modem transport stopped"
	if clientIDsExhausted {
		message = "modem QMI client IDs exhausted"
	}
	slog.Warn(message, "imei", failure.modem.EquipmentIdentifier, "generation", failure.modem.Generation(), "error", failure.err)
	r.removeModem(key, failure.modem)
	if ctx.Err() != nil {
		return
	}
	if recoverySuspended {
		slog.Error("suspend modem recovery until device reconnects", "imei", failure.modem.EquipmentIdentifier, "generation", failure.modem.Generation(), "error", failure.err)
		return
	}
	if err := r.recoverRemovedModem(ctx, failure.modem); err != nil {
		slog.Error("recover modem after transport stop", "imei", failure.modem.EquipmentIdentifier, "generation", failure.modem.Generation(), "error", err)
	}
}

func (r *Registry) reloadModem(ctx context.Context, current *Modem) (*Modem, error) {
	if current == nil {
		return nil, errModemRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	key := r.keyForModemLocked(current)
	replacement := r.newerGenerationLocked(current)
	r.mu.RUnlock()
	if key == "" {
		if replacement != nil {
			return replacement, nil
		}
		return nil, fmt.Errorf("%w: modem generation %d", ErrNotFound, current.Generation())
	}

	slog.Info("reload modem generation", "imei", current.EquipmentIdentifier, "generation", current.Generation())
	r.removeModem(key, current)
	if err := r.recoverRemovedModem(ctx, current); err != nil {
		return nil, err
	}

	r.mu.RLock()
	replacement = r.newerGenerationLocked(current)
	r.mu.RUnlock()
	if replacement == nil {
		return nil, fmt.Errorf("%w: replacement for modem generation %d", ErrNotFound, current.Generation())
	}
	return replacement, nil
}

func (r *Registry) recoverRemovedModem(ctx context.Context, failed *Modem) error {
	var result error
	for attempt := range registryFailureRecoveryAttempts {
		if attempt > 0 {
			if err := sleepContext(ctx, registryWatchRetryDelay); err != nil {
				return errors.Join(result, err)
			}
		}
		if err := r.reconcile(ctx); err != nil {
			result = errors.Join(result, err)
		}
		current, err := r.findModem(failed.EquipmentIdentifier)
		if err == nil && samePhysicalModem(failed, current) {
			return nil
		}
		if err == nil {
			err = errors.New("recovered modem does not match failed physical device")
		}
		result = errors.Join(result, err)
		if r.cidRecoveryState(failed.Path()) == cidRecoverySuspended {
			break
		}
	}
	return result
}

func (r *Registry) removeModem(key string, existing *Modem) {
	if existing == nil {
		return
	}
	r.mu.Lock()
	if current := r.modems[key]; current != existing {
		key = r.keyForModemLocked(existing)
	}
	if key == "" || r.modems[key] != existing {
		r.mu.Unlock()
		return
	}
	delete(r.modems, key)
	delete(r.simIdentities, existing)
	snapshot := r.copyModemsLocked()
	subscribers := append([]subscription(nil), r.subs...)
	r.mu.Unlock()
	r.publish(subscribers, ModemEvent{Type: ModemEventRemoved, Modem: existing, Path: key, Generation: existing.Generation(), Snapshot: snapshot})
	if err := existing.Close(); err != nil {
		slog.Warn("close removed modem", "path", key, "error", err)
	}
}

func (r *Registry) findByDeviceLocked(device wwanmodem.Device) (string, *Modem) {
	key := physicalDeviceKey(device)
	if modem := r.modems[key]; modem != nil {
		return key, modem
	}
	for candidateKey, modem := range r.modems {
		if modem != nil && sameControlDevice(modem.deviceInfo, device) {
			return candidateKey, modem
		}
	}
	return "", nil
}

func (r *Registry) findReplacementLocked(key string, replacement *Modem) (string, *Modem) {
	if modem := r.modems[key]; modem != nil {
		return key, modem
	}
	for candidateKey, modem := range r.modems {
		if samePhysicalModem(modem, replacement) {
			return candidateKey, modem
		}
	}
	return "", nil
}

func (r *Registry) keyForModemLocked(target *Modem) string {
	for key, modem := range r.modems {
		if modem == target {
			return key
		}
	}
	return ""
}

func (r *Registry) newerGenerationLocked(current *Modem) *Modem {
	for _, candidate := range r.modems {
		if candidate != nil && candidate.Generation() > current.Generation() && samePhysicalModem(current, candidate) {
			return candidate
		}
	}
	return nil
}

func (r *Registry) publish(subscribers []subscription, event ModemEvent) {
	for _, subscriber := range subscribers {
		if err := subscriber.fn(event); err != nil {
			slog.Error("process modem event", "type", event.Type, "path", event.Path, "error", err)
		}
	}
}

func (r *Registry) nextGenerationToken() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextGeneration++
	return r.nextGeneration
}

func (r *Registry) cidRecoveryState(key string) cidRecoveryState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cidRecoveryStates[key]
}

func (r *Registry) advanceCIDRecoveryState(key string) cidRecoveryState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cidRecoveryStates == nil {
		r.cidRecoveryStates = make(map[string]cidRecoveryState)
	}
	state := cidRecoveryRetried
	if r.cidRecoveryStates[key] >= cidRecoveryRetried {
		state = cidRecoverySuspended
	}
	r.cidRecoveryStates[key] = state
	return state
}

func (r *Registry) clearCIDRecoveryState(key string) {
	r.mu.Lock()
	delete(r.cidRecoveryStates, key)
	r.mu.Unlock()
}

func (r *Registry) clearAbsentCIDRecoveryStates(present map[string]wwanmodem.Device) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key := range r.cidRecoveryStates {
		if _, ok := present[key]; !ok {
			delete(r.cidRecoveryStates, key)
		}
	}
}
