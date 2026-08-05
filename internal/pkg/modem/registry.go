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
	wg             sync.WaitGroup

	discover     func(context.Context) ([]wwanmodem.Device, error)
	watchDevices func(context.Context) (<-chan wwanmodem.Result[wwanmodem.DeviceEvent], error)
	open         func(context.Context, wwanmodem.Device, uint64) (*Modem, error)
	openDevice   deviceControlOpener
	failures     chan modemFailure
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
		cidRecoveryStates: make(map[string]cidRecoveryState),
		simIdentities:     make(map[*Modem]simIdentity),
	}, nil
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
	return r.findModem(id)
}

func (r *Registry) Subscribe(fn func(ModemEvent) error) (func(), error) {
	if fn == nil {
		return nil, errors.New("modem subscriber is required")
	}
	if err := r.ensureStarted(context.Background()); err != nil {
		return nil, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("modem registry is closed")
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
	unsubscribe, err := r.Subscribe(func(event ModemEvent) error {
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
	if candidate, err := r.findModem(current.EquipmentIdentifier); err == nil && candidate.Generation() > current.Generation() {
		return candidate, nil
	}
	select {
	case modem := <-ready:
		return modem, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *Registry) findModem(id string) (*Modem, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: equipment identifier is empty", ErrNotFound)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, modem := range r.modems {
		if strings.TrimSpace(modem.EquipmentIdentifier) == id {
			return modem, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

func (r *Registry) copyModemsLocked() map[string]*Modem { return maps.Clone(r.modems) }
