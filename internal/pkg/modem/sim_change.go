package modem

import (
	"context"
	"errors"
	"fmt"
	"strings"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

// simChange keeps request lookups separate from the registry's immediate
// lifecycle observations. Subscribers must still release retired clients while
// a profile operation waits for its replacement generation.
type simChange struct {
	done   chan struct{}
	refs   int
	cancel context.CancelCauseFunc
	modem  *Modem
}

// WithSIMChange serializes SIM changes for one device across modem generations.
// Find and ReadyModems wait until run completes. run receives the current modem
// and must use EnsureSIMVisible to follow replacements rather than call Find.
// Callers must bound ctx, and run must honor its cancellation.
func (r *Registry) WithSIMChange(ctx context.Context, current *Modem, run func(context.Context, *Modem) error) error {
	if current == nil {
		return errModemRequired
	}
	if err := r.ensureStarted(ctx); err != nil {
		return err
	}
	id := strings.TrimSpace(current.EquipmentIdentifier)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return errRegistryClosed
		}
		if pending := r.simChanges[id]; pending != nil {
			r.mu.Unlock()
			if err := waitForSIMChange(ctx, pending); err != nil {
				return err
			}
			continue
		}
		active, err := r.findModemLocked(id)
		if err != nil {
			r.mu.Unlock()
			return err
		}
		changeCtx, cancel := context.WithCancelCause(ctx)
		change := r.holdSIMChangeLocked(active)
		change.cancel = cancel
		r.mu.Unlock()
		defer cancel(nil)
		defer r.releaseSIMChange(id, change)
		err = run(changeCtx, active)
		if cause := context.Cause(changeCtx); cause != nil && !errors.Is(err, cause) {
			return errors.Join(err, cause)
		}
		return err
	}
}

// ReadyModems returns an inventory after pending SIM changes have settled.
// Internal lifecycle subscribers use Modems, which never waits for a change.
func (r *Registry) ReadyModems(ctx context.Context) (map[string]*Modem, error) {
	if err := r.ensureStarted(ctx); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.mu.RLock()
		if r.closed {
			r.mu.RUnlock()
			return nil, errRegistryClosed
		}
		var pending *simChange
		for _, change := range r.simChanges {
			pending = change
			break
		}
		if pending == nil {
			modems := r.copyModemsLocked()
			r.mu.RUnlock()
			return modems, nil
		}
		r.mu.RUnlock()
		if err := waitForSIMChange(ctx, pending); err != nil {
			return nil, err
		}
	}
}

func (r *Registry) findReadyModem(ctx context.Context, id string) (*Modem, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.mu.RLock()
		if r.closed {
			r.mu.RUnlock()
			return nil, errRegistryClosed
		}
		pending := r.simChanges[id]
		if pending == nil {
			modem, err := r.findModemLocked(id)
			r.mu.RUnlock()
			return modem, err
		}
		r.mu.RUnlock()
		if err := waitForSIMChange(ctx, pending); err != nil {
			return nil, err
		}
	}
}

func waitForSIMChange(ctx context.Context, change *simChange) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-change.done:
		return nil
	}
}

func (r *Registry) holdSIMChangeLocked(current *Modem) *simChange {
	id := strings.TrimSpace(current.EquipmentIdentifier)
	if r.simChanges == nil {
		r.simChanges = make(map[string]*simChange)
	}
	change := r.simChanges[id]
	if change == nil {
		change = &simChange{done: make(chan struct{}), modem: current}
		r.simChanges[id] = change
	}
	change.refs++
	return change
}

func (r *Registry) releaseSIMChange(id string, change *simChange) {
	id = strings.TrimSpace(id)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.simChanges[id] != change {
		return
	}
	change.refs--
	if change.refs == 0 {
		delete(r.simChanges, id)
		close(change.done)
	}
}

func (r *Registry) abortSIMChangeLocked(id string, err error) {
	id = strings.TrimSpace(id)
	change := r.simChanges[id]
	if change == nil {
		return
	}
	delete(r.simChanges, id)
	if change.cancel != nil {
		change.cancel(err)
	}
	close(change.done)
}

func (r *Registry) abortRemovedSIMChanges(device wwanmodem.Device) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, change := range r.simChanges {
		if physicalDeviceKey(change.modem.deviceInfo) == physicalDeviceKey(device) || sameControlDevice(change.modem.deviceInfo, device) {
			r.abortSIMChangeLocked(id, fmt.Errorf("%w: %s", ErrNotFound, id))
		}
	}
}

func (r *Registry) abortAbsentSIMChanges(present map[string]wwanmodem.Device) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, change := range r.simChanges {
		if _, ok := present[physicalDeviceKey(change.modem.deviceInfo)]; !ok && !devicePresentInSnapshot(change.modem.deviceInfo, present) {
			r.abortSIMChangeLocked(id, fmt.Errorf("%w: %s", ErrNotFound, id))
		}
	}
}
