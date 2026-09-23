package network

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/sigmo/internal/pkg/modemtask"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// RegistrationMode names how the modem chooses its network.
type RegistrationMode string

const (
	// RegistrationModeAutomatic lets the modem pick a network on its own.
	RegistrationModeAutomatic RegistrationMode = "automatic"
	// RegistrationModeManual pins the modem to one operator until released.
	RegistrationModeManual RegistrationMode = "manual"
)

var (
	errOperatorCodeRequired               = errors.New("operator code is required")
	errRegistrationModeInvalid            = errors.New("registration mode is invalid")
	errNetworkRegistrationStorageRequired = errors.New("network registration storage is required")
)

const (
	networkRegistrationKey     = "network.registration"
	networkRegistrationPrefix  = "profile:"
	networkRegistrationModemID = "modem:"
)

var networkRegistrationRestoreRetryInterval = 5 * time.Second

// networkRegistrationPreference is saved per SIM profile. A manual selection
// is a property of one subscription: it must follow that profile and must be
// released before another profile uses the same modem.
type networkRegistrationPreference struct {
	Mode         RegistrationMode `json:"mode"`
	OperatorCode string           `json:"operatorCode"`
}

func (p networkRegistrationPreference) manual() bool {
	return p.Mode == RegistrationModeManual && p.OperatorCode != ""
}

// networkRegistrar is the slice of modem behavior registration depends on.
// It exists so preference and restore logic can be exercised with a fake
// instead of a live QMI or MBIM session.
type networkRegistrar interface {
	Selection(context.Context, *mmodem.Modem) (wwan.NetworkSelection, error)
	OperatorCode(context.Context, *mmodem.Modem) (string, error)
	Register(context.Context, *mmodem.Modem, string) error
	RegisterAutomatically(context.Context, *mmodem.Modem) error
}

// modemRegistrar forwards to the modem's 3GPP adapter.
type modemRegistrar struct{}

func (modemRegistrar) Selection(ctx context.Context, modem *mmodem.Modem) (wwan.NetworkSelection, error) {
	return modem.ThreeGPP().NetworkSelection(ctx)
}

func (modemRegistrar) OperatorCode(ctx context.Context, modem *mmodem.Modem) (string, error) {
	return modem.ThreeGPP().OperatorCode(ctx)
}

func (modemRegistrar) Register(ctx context.Context, modem *mmodem.Modem, operatorCode string) error {
	return modem.ThreeGPP().RegisterNetwork(ctx, operatorCode)
}

func (modemRegistrar) RegisterAutomatically(ctx context.Context, modem *mmodem.Modem) error {
	return modem.ThreeGPP().RegisterNetworkAutomatically(ctx)
}

// readSelection reads the modem's selection preference. Modems that cannot
// report it (AT-only control, old firmware) yield the zero value so callers
// fall back to the saved preference instead of failing.
func readSelection(ctx context.Context, registrar networkRegistrar, modem *mmodem.Modem) (wwan.NetworkSelection, error) {
	selection, err := registrar.Selection(ctx, modem)
	if errors.Is(err, wwan.ErrUnsupported) {
		return wwan.NetworkSelection{}, nil
	}
	if err != nil {
		return wwan.NetworkSelection{}, fmt.Errorf("read network selection: %w", err)
	}
	return selection, nil
}

// Registration reports the selection the modem currently applies. The modem
// answer wins over the saved preference because firmware keeps a manual
// selection across profile switches and restarts, which is exactly the state
// the UI needs to surface.
func (n *network) Registration(ctx context.Context, modem *mmodem.Modem) (*RegistrationResponse, error) {
	saved, err := loadRegistrationPreference(ctx, n.store, modem)
	if err != nil {
		return nil, fmt.Errorf("load network registration: %w", err)
	}
	selection, err := readSelection(ctx, n.registrar, modem)
	if err != nil {
		return nil, err
	}
	switch selection.Mode {
	case wwan.NetworkSelectionAutomatic:
		return &RegistrationResponse{Mode: RegistrationModeAutomatic}, nil
	case wwan.NetworkSelectionManual:
		operatorCode := selection.OperatorID
		if operatorCode == "" && saved.manual() {
			operatorCode = saved.OperatorCode
		}
		return &RegistrationResponse{Mode: RegistrationModeManual, OperatorCode: operatorCode}, nil
	default:
		if saved.manual() {
			return &RegistrationResponse{Mode: RegistrationModeManual, OperatorCode: saved.OperatorCode}, nil
		}
		return &RegistrationResponse{Mode: RegistrationModeAutomatic}, nil
	}
}

func (n *network) SetRegistration(ctx context.Context, modem *mmodem.Modem, req SetRegistrationRequest) error {
	switch req.Mode {
	case RegistrationModeAutomatic:
		return n.registerAutomatically(ctx, modem)
	case RegistrationModeManual:
		return n.registerManually(ctx, modem, req.OperatorCode)
	default:
		return errRegistrationModeInvalid
	}
}

func (n *network) registerManually(ctx context.Context, modem *mmodem.Modem, operatorCode string) error {
	operatorCode = strings.TrimSpace(operatorCode)
	if operatorCode == "" {
		return errOperatorCodeRequired
	}
	if err := n.registrar.Register(ctx, modem, operatorCode); err != nil {
		return fmt.Errorf("register network %s: %w", operatorCode, err)
	}
	n.InvalidateScan(modem)
	if err := n.saveRegistration(ctx, modem, networkRegistrationPreference{
		Mode:         RegistrationModeManual,
		OperatorCode: operatorCode,
	}); err != nil {
		return fmt.Errorf("save network registration: %w", err)
	}
	return nil
}

// registerAutomatically hands selection back to the modem and records that
// choice so a restart or profile switch does not resurrect the old operator.
func (n *network) registerAutomatically(ctx context.Context, modem *mmodem.Modem) error {
	if err := n.registrar.RegisterAutomatically(ctx, modem); err != nil {
		return fmt.Errorf("register network automatically: %w", err)
	}
	n.InvalidateScan(modem)
	if err := n.saveRegistration(ctx, modem, networkRegistrationPreference{Mode: RegistrationModeAutomatic}); err != nil {
		return fmt.Errorf("save network registration: %w", err)
	}
	return nil
}

func (n *network) saveRegistration(ctx context.Context, modem *mmodem.Modem, pref networkRegistrationPreference) error {
	scope := registrationScope(modem)
	if scope == "" {
		return nil
	}
	return n.store.Put(ctx, scope, networkRegistrationKey, pref)
}

func loadRegistrationPreference(ctx context.Context, store *storage.Store, modem *mmodem.Modem) (networkRegistrationPreference, error) {
	scope := registrationScope(modem)
	if scope == "" {
		return networkRegistrationPreference{}, nil
	}
	var pref networkRegistrationPreference
	err := store.Get(ctx, scope, networkRegistrationKey, &pref)
	if errors.Is(err, storage.ErrNotFound) {
		return networkRegistrationPreference{}, nil
	}
	if err != nil {
		return networkRegistrationPreference{}, err
	}
	pref.OperatorCode = strings.TrimSpace(pref.OperatorCode)
	return pref, nil
}

// RunRegistrationRestore reapplies each SIM profile's selection preference.
// It runs once per modem generation and again whenever the active profile
// changes, because a manual selection left behind by the previous profile
// would otherwise keep the new profile off its own network.
func RunRegistrationRestore(ctx context.Context, registry modemtask.Registry, store *storage.Store) error {
	if store == nil {
		return errNetworkRegistrationStorageRequired
	}
	if registry == nil {
		return errors.New("modem registry is required")
	}
	restorer := newRegistrationRestorer(store)
	unsubscribe, err := registry.Subscribe(ctx, func(event mmodem.ModemEvent) error {
		if event.Type == mmodem.ModemEventSIMChanged {
			restorer.notify(event.Modem)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("subscribe SIM changes: %w", err)
	}
	defer unsubscribe()
	return modemtask.Run(ctx, registry, restorer.run)
}

type registrationRestorer struct {
	store     *storage.Store
	registrar networkRegistrar
	mu        sync.Mutex
	triggers  map[*mmodem.Modem]chan struct{}
}

func newRegistrationRestorer(store *storage.Store) *registrationRestorer {
	return &registrationRestorer{
		store:     store,
		registrar: modemRegistrar{},
		triggers:  make(map[*mmodem.Modem]chan struct{}),
	}
}

// run serializes restores per modem: a SIM change observed while a restore is
// in flight queues exactly one follow-up pass instead of racing it.
func (r *registrationRestorer) run(ctx context.Context, modem *mmodem.Modem) {
	trigger := make(chan struct{}, 1)
	r.mu.Lock()
	r.triggers[modem] = trigger
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		if r.triggers[modem] == trigger {
			delete(r.triggers, modem)
		}
		r.mu.Unlock()
	}()
	for {
		r.restoreWithRetry(ctx, modem)
		select {
		case <-ctx.Done():
			return
		case <-trigger:
		}
	}
}

func (r *registrationRestorer) notify(modem *mmodem.Modem) {
	if modem == nil {
		return
	}
	r.mu.Lock()
	trigger := r.triggers[modem]
	r.mu.Unlock()
	if trigger == nil {
		return
	}
	select {
	case trigger <- struct{}{}:
	default:
	}
}

func (r *registrationRestorer) restoreWithRetry(ctx context.Context, modem *mmodem.Modem) {
	warned := false
	for {
		err := r.restoreModem(ctx, modem)
		if err == nil || ctx.Err() != nil {
			return
		}
		if warned {
			slog.Debug("retry network registration restore", "imei", modem.EquipmentIdentifier, "error", err)
		} else {
			slog.Warn("restore network registration", "imei", modem.EquipmentIdentifier, "error", err)
			warned = true
		}
		if err := sleepContext(ctx, networkRegistrationRestoreRetryInterval); err != nil {
			return
		}
	}
}

func (r *registrationRestorer) restoreModem(ctx context.Context, modem *mmodem.Modem) error {
	pref, err := loadRegistrationPreference(ctx, r.store, modem)
	if err != nil {
		return err
	}
	selection, err := readSelection(ctx, r.registrar, modem)
	if err != nil {
		return err
	}
	if pref.manual() {
		return r.restoreManual(ctx, modem, selection, pref.OperatorCode)
	}
	// Anything short of an explicit manual preference means this profile
	// expects the modem to choose. Only an observed manual selection needs
	// to be released; an unknown selection is left alone.
	if selection.Mode != wwan.NetworkSelectionManual {
		return nil
	}
	if err := r.registrar.RegisterAutomatically(ctx, modem); err != nil {
		return fmt.Errorf("register network automatically: %w", err)
	}
	slog.Info("network selection restored", "imei", modem.EquipmentIdentifier, "mode", RegistrationModeAutomatic)
	return nil
}

func (r *registrationRestorer) restoreManual(ctx context.Context, modem *mmodem.Modem, selection wwan.NetworkSelection, operatorCode string) error {
	switch selection.Mode {
	case wwan.NetworkSelectionManual:
		if selection.OperatorID == operatorCode {
			return nil
		}
	case wwan.NetworkSelectionUnknown:
		// Without a readable selection the registered operator is the best
		// signal that the manual choice survived.
		current, err := r.registrar.OperatorCode(ctx, modem)
		if err == nil && strings.TrimSpace(current) == operatorCode {
			return nil
		}
	}
	if err := r.registrar.Register(ctx, modem, operatorCode); err != nil {
		return fmt.Errorf("register network %s: %w", operatorCode, err)
	}
	slog.Info("network selection restored", "imei", modem.EquipmentIdentifier, "mode", RegistrationModeManual, "operator", operatorCode)
	return nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func registrationScope(modem *mmodem.Modem) string {
	if sim := modem.Snapshot().SIM; sim != nil {
		if profileID := strings.TrimSpace(sim.Identifier); profileID != "" {
			return networkRegistrationPrefix + profileID
		}
	}
	if modemID := strings.TrimSpace(modem.EquipmentIdentifier); modemID != "" {
		return networkRegistrationModemID + modemID
	}
	return ""
}
