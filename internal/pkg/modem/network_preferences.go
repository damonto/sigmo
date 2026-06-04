package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

var networkPreferencesRetryInterval = 5 * time.Second

var errNetworkPreferencesStorageRequired = errors.New("network preferences storage is required")

type NetworkPreferences struct {
	store *storage.Store
	mu    sync.Mutex
}

type networkPreferenceMode struct {
	Allowed   ModemMode `json:"allowed"`
	Preferred ModemMode `json:"preferred"`
}

type savedNetworkPreferences struct {
	Mode  *networkPreferenceMode `json:"mode,omitempty"`
	Bands []ModemBand            `json:"bands,omitempty"`
}

func NewNetworkPreferences(store *storage.Store) (*NetworkPreferences, error) {
	if store == nil {
		return nil, errNetworkPreferencesStorageRequired
	}
	return &NetworkPreferences{store: store}, nil
}

func (p *NetworkPreferences) SaveMode(ctx context.Context, modemID string, mode ModemModePair) error {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return errors.New("modem id is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	entry, _, err := p.loadForModemLocked(ctx, modemID)
	if err != nil {
		return err
	}
	entry.Mode = &networkPreferenceMode{
		Allowed:   mode.Allowed,
		Preferred: mode.Preferred,
	}
	return p.saveForModemLocked(ctx, modemID, entry)
}

func (p *NetworkPreferences) SaveBands(ctx context.Context, modemID string, bands []ModemBand) error {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return errors.New("modem id is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	entry, _, err := p.loadForModemLocked(ctx, modemID)
	if err != nil {
		return err
	}
	entry.Bands = slices.Clone(bands)
	return p.saveForModemLocked(ctx, modemID, entry)
}

func (p *NetworkPreferences) Run(ctx context.Context, registry *Registry) error {
	task := newPresenceTask(registry, p.restoreWithRetry)
	return task.Run(ctx)
}

func (p *NetworkPreferences) restoreWithRetry(ctx context.Context, m *Modem) {
	warned := false
	for {
		retry, err := p.restoreOnce(ctx, m)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		if !retry {
			slog.Warn("restore network preferences", "modem", m.EquipmentIdentifier, "error", err)
			return
		}
		if warned {
			slog.Debug("retry network preferences restore", "modem", m.EquipmentIdentifier, "error", err)
		} else {
			slog.Warn("restore network preferences", "modem", m.EquipmentIdentifier, "error", err)
			warned = true
		}
		if err := sleepContext(ctx, networkPreferencesRetryInterval); err != nil {
			return
		}
	}
}

func (p *NetworkPreferences) restoreOnce(ctx context.Context, m *Modem) (bool, error) {
	prefs, ok, err := p.loadForModem(ctx, m.EquipmentIdentifier)
	if err != nil {
		return false, fmt.Errorf("load network preferences: %w", err)
	}
	if !ok {
		return false, nil
	}

	var result error
	retry := false
	if prefs.Mode != nil {
		mode := ModemModePair{
			Allowed:   prefs.Mode.Allowed,
			Preferred: prefs.Mode.Preferred,
		}
		nextRetry, err := restoreModePreference(ctx, m, mode)
		if err != nil {
			result = errors.Join(result, err)
			retry = retry || nextRetry
		}
	}
	if prefs.Bands != nil {
		nextRetry, err := restoreBandPreference(ctx, m, prefs.Bands)
		if err != nil {
			result = errors.Join(result, err)
			retry = retry || nextRetry
		}
	}
	return retry, result
}

func (p *NetworkPreferences) loadForModem(ctx context.Context, modemID string) (savedNetworkPreferences, bool, error) {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return savedNetworkPreferences{}, false, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	return p.loadForModemLocked(ctx, modemID)
}

func (p *NetworkPreferences) loadForModemLocked(ctx context.Context, modemID string) (savedNetworkPreferences, bool, error) {
	var entry savedNetworkPreferences
	err := p.store.Get(ctx, "modem:"+modemID, "network.preferences", &entry)
	if errors.Is(err, storage.ErrNotFound) {
		return savedNetworkPreferences{}, false, nil
	}
	if err != nil {
		return savedNetworkPreferences{}, false, err
	}
	return entry, true, nil
}

func (p *NetworkPreferences) saveForModemLocked(ctx context.Context, modemID string, entry savedNetworkPreferences) error {
	return p.store.Put(ctx, "modem:"+modemID, "network.preferences", entry)
}

func restoreModePreference(ctx context.Context, m *Modem, mode ModemModePair) (bool, error) {
	supported, err := m.SupportedModes(ctx)
	if err != nil {
		return isTransientRestartError(err), fmt.Errorf("read supported modes: %w", err)
	}
	if !slices.Contains(supported, mode) {
		return false, fmt.Errorf("saved mode unsupported: allowed=%d preferred=%d", mode.Allowed, mode.Preferred)
	}

	current, err := m.CurrentModes(ctx)
	if err != nil {
		return isTransientRestartError(err), fmt.Errorf("read current modes: %w", err)
	}
	if current == mode {
		return false, nil
	}
	if err := m.SetCurrentModes(ctx, mode); err != nil {
		return isTransientRestartError(err), fmt.Errorf("set current modes: %w", err)
	}
	slog.Info("network mode restored", "modem", m.EquipmentIdentifier, "allowed", mode.Allowed, "preferred", mode.Preferred)
	return false, nil
}

func restoreBandPreference(ctx context.Context, m *Modem, bands []ModemBand) (bool, error) {
	if len(bands) == 0 {
		return false, errors.New("saved bands are empty")
	}
	if duplicateBand(bands) {
		return false, errors.New("saved bands contain duplicates")
	}
	if slices.Contains(bands, ModemBandAny) && len(bands) > 1 {
		return false, errors.New("saved bands combine any with other bands")
	}

	supported, err := m.SupportedBands(ctx)
	if err != nil {
		return isTransientRestartError(err), fmt.Errorf("read supported bands: %w", err)
	}
	for _, band := range bands {
		if !slices.Contains(supported, band) {
			return false, fmt.Errorf("saved band unsupported: %d", band)
		}
	}

	current, err := m.CurrentBands(ctx)
	if err != nil {
		return isTransientRestartError(err), fmt.Errorf("read current bands: %w", err)
	}
	if sameBands(current, bands) {
		return false, nil
	}
	if err := m.SetCurrentBands(ctx, bands); err != nil {
		return isTransientRestartError(err), fmt.Errorf("set current bands: %w", err)
	}
	slog.Info("network bands restored", "modem", m.EquipmentIdentifier, "bands", bands)
	return false, nil
}

func sameBands(a []ModemBand, b []ModemBand) bool {
	if len(a) != len(b) {
		return false
	}
	if duplicateBand(a) || duplicateBand(b) {
		return false
	}
	for _, band := range a {
		if !slices.Contains(b, band) {
			return false
		}
	}
	return true
}

func duplicateBand(bands []ModemBand) bool {
	seen := make(map[ModemBand]struct{}, len(bands))
	for _, band := range bands {
		if _, ok := seen[band]; ok {
			return true
		}
		seen[band] = struct{}{}
	}
	return false
}
