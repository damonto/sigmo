package modem

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

var (
	registryOpenTimeout     = 15 * time.Second
	registryWatchRetryDelay = time.Second
)

const (
	qualcomm410ControlPortResolveAttempts = 10
	qualcomm410ControlPortResolveInterval = 50 * time.Millisecond
)

var (
	ErrNotFound                          = errors.New("modem not found")
	errModemRequired                     = errors.New("modem is required")
	errRegistryClosed                    = errors.New("modem registry is closed")
	errQualcomm410Data5PortNotDiscovered = errors.New("Qualcomm 410 DATA5 QMI control port is not mapped to a discovered port")
)

type Registry struct {
	mu             sync.RWMutex
	startMu        sync.Mutex
	modems         map[string]*Modem
	subs           []subscription
	nextSubID      uint64
	nextGeneration uint64
	started        bool
	closed         bool
	cancel         context.CancelFunc
	done           chan struct{}
	wg             sync.WaitGroup

	discover     func(context.Context) ([]wwanmodem.Device, error)
	watchDevices func(context.Context) (<-chan wwanmodem.Result[wwanmodem.DeviceEvent], error)
	open         func(context.Context, wwanmodem.Device, uint64) (*Modem, error)
	openDevice   deviceControlOpener
	failures     chan modemFailure
	reloads      chan modemReloadRequest
	// reloading keeps stable IMEI lookups in the replacement window.
	reloading map[string]chan struct{}
	// A physical reconnect resets the bounded CID-exhaustion recovery state.
	cidRecoveryStates map[string]cidRecoveryState
	simIdentities     map[*Modem]simIdentity
}

type cidRecoveryState uint8

const (
	cidRecoveryUnused cidRecoveryState = iota
	cidRecoveryRetried
	cidRecoverySuspended
)

type modemFailure struct {
	modem *Modem
	err   error
}

type modemReloadRequest struct {
	modem  *Modem
	result chan modemReloadResult
}

type modemReloadResult struct {
	modem *Modem
	err   error
}

type ModemEventType int

const (
	ModemEventAdded ModemEventType = iota
	ModemEventRemoved
	ModemEventChanged
	ModemEventSIMChanged
)

func (t ModemEventType) String() string {
	switch t {
	case ModemEventAdded:
		return "added"
	case ModemEventRemoved:
		return "removed"
	case ModemEventChanged:
		return "changed"
	case ModemEventSIMChanged:
		return "sim-changed"
	default:
		return "unknown"
	}
}

type ModemEvent struct {
	Type                  ModemEventType
	Modem                 *Modem
	Previous              *Modem
	Path                  string
	PreviousPath          string
	Generation            uint64
	PreviousSIMSlot       uint32
	SIMSlot               uint32
	PreviousSIMIdentifier string
	SIMIdentifier         string
	Snapshot              map[string]*Modem
}

type subscription struct {
	id uint64
	fn func(ModemEvent) error
}

func NewRegistry() (*Registry, error) {
	return &Registry{
		modems:            make(map[string]*Modem),
		discover:          wwanmodem.Discover,
		watchDevices:      wwanmodem.WatchDevices,
		open:              openDiscoveredModem,
		failures:          make(chan modemFailure, 32),
		reloads:           make(chan modemReloadRequest, 8),
		reloading:         make(map[string]chan struct{}),
		done:              make(chan struct{}),
		cidRecoveryStates: make(map[string]cidRecoveryState),
		simIdentities:     make(map[*Modem]simIdentity),
	}, nil
}

// Start performs initial discovery using ctx and then owns the device watcher
// until Close is called.
func (r *Registry) Start(ctx context.Context) error {
	return r.ensureStarted(ctx)
}

func (r *Registry) Modems(ctx context.Context) (map[string]*Modem, error) {
	if err := r.ensureStarted(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.copyModemsLocked(), nil
}

func (r *Registry) Find(ctx context.Context, id string) (*Modem, error) {
	if err := r.ensureStarted(ctx); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: equipment identifier is empty", ErrNotFound)
	}
	for {
		r.mu.RLock()
		current := r.findModemLocked(id)
		reloadDone := r.reloading[id]
		closed := r.closed
		done := r.done
		r.mu.RUnlock()
		if closed {
			return nil, errRegistryClosed
		}
		if reloadDone != nil {
			select {
			case <-reloadDone:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				return nil, errRegistryClosed
			}
		}
		if current != nil {
			return current, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
}

// Reload retires one modem generation and waits for Registry to open its
// replacement. The device loop owns this transition so a device event cannot
// open another generation before the old QMI clients have released their IDs.
func (r *Registry) Reload(ctx context.Context, current *Modem) (*Modem, error) {
	if current == nil {
		return nil, errModemRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.ensureStarted(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	closed := r.closed
	reloads := r.reloads
	done := r.done
	r.mu.RUnlock()
	if closed {
		return nil, errRegistryClosed
	}
	if reloads == nil {
		return nil, errors.New("modem registry reload queue is unavailable")
	}

	result := make(chan modemReloadResult, 1)
	request := modemReloadRequest{modem: current, result: result}
	select {
	case reloads <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
		return nil, errRegistryClosed
	}

	select {
	case reloaded := <-result:
		if reloaded.err != nil {
			return nil, fmt.Errorf("reload modem generation: %w", reloaded.err)
		}
		return reloaded.modem, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
		return nil, errRegistryClosed
	}
}

// Subscribe registers fn after ensuring the registry has started. ctx only
// controls startup; the subscription remains active until its returned
// function is called or the registry closes.
func (r *Registry) Subscribe(ctx context.Context, fn func(ModemEvent) error) (func(), error) {
	if fn == nil {
		return nil, errors.New("modem subscriber is required")
	}
	if err := r.ensureStarted(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errRegistryClosed
	}
	r.nextSubID++
	id := r.nextSubID
	r.subs = append(r.subs, subscription{id: id, fn: fn})
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			for i := range r.subs {
				if r.subs[i].id == id {
					r.subs = append(r.subs[:i], r.subs[i+1:]...)
					break
				}
			}
			r.mu.Unlock()
		})
	}, nil
}

func (r *Registry) Close() error {
	r.startMu.Lock()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		r.startMu.Unlock()
		return nil
	}
	r.closed = true
	if r.done != nil {
		close(r.done)
	}
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	r.startMu.Unlock()
	r.wg.Wait()
	r.mu.Lock()
	modems := maps.Clone(r.modems)
	r.modems = make(map[string]*Modem)
	r.subs = nil
	r.simIdentities = make(map[*Modem]simIdentity)
	r.mu.Unlock()
	var result error
	for _, modem := range modems {
		result = errors.Join(result, modem.Close())
	}
	return result
}

func (r *Registry) WaitForReloadedModem(ctx context.Context, current *Modem) (*Modem, error) {
	if current == nil {
		return nil, errModemRequired
	}
	ready := make(chan *Modem, 1)
	unsubscribe, err := r.Subscribe(ctx, func(event ModemEvent) error {
		if event.Modem != nil && samePhysicalModem(current, event.Modem) && event.Generation > current.Generation() {
			select {
			case ready <- event.Modem:
			default:
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	defer unsubscribe()
	r.mu.RLock()
	done := r.done
	r.mu.RUnlock()
	if candidate, err := r.findModem(current.EquipmentIdentifier); err == nil && candidate.Generation() > current.Generation() {
		return candidate, nil
	}
	select {
	case modem := <-ready:
		return modem, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
		return nil, errRegistryClosed
	}
}

func (r *Registry) findModem(id string) (*Modem, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: equipment identifier is empty", ErrNotFound)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if current := r.findModemLocked(id); current != nil {
		return current, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

func (r *Registry) findModemLocked(id string) *Modem {
	for _, modem := range r.modems {
		if strings.TrimSpace(modem.EquipmentIdentifier) == id {
			return modem
		}
	}
	return nil
}

func (r *Registry) copyModemsLocked() map[string]*Modem { return maps.Clone(r.modems) }
