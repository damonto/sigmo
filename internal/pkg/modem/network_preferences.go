package modem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	networkPreferencesStateDirName  = "sigmo"
	networkPreferencesStateFileName = "network-preferences.json"
	networkPreferencesStateVersion  = 1
)

var networkPreferencesRetryInterval = 5 * time.Second

type NetworkPreferences struct {
	path string
	mu   sync.Mutex
}

type networkPreferenceMode struct {
	Allowed   ModemMode `json:"allowed"`
	Preferred ModemMode `json:"preferred"`
}

type savedNetworkPreferences struct {
	Mode  *networkPreferenceMode `json:"mode,omitempty"`
	Bands []ModemBand            `json:"bands,omitempty"`
}

type networkPreferencesStateFile struct {
	Version int                                `json:"version"`
	Modems  map[string]savedNetworkPreferences `json:"modems"`
}

func NewNetworkPreferences() (*NetworkPreferences, error) {
	path, err := defaultNetworkPreferencesPath()
	if err != nil {
		return nil, fmt.Errorf("resolve network preferences state path: %w", err)
	}
	return NewNetworkPreferencesWithPath(path), nil
}

func NewNetworkPreferencesWithPath(path string) *NetworkPreferences {
	return &NetworkPreferences{path: path}
}

func defaultNetworkPreferencesPath() (string, error) {
	return networkPreferencesPathFromEnv(os.LookupEnv, os.UserHomeDir)
}

func networkPreferencesPathFromEnv(lookupEnv func(string) (string, bool), userHomeDir func() (string, error)) (string, error) {
	if stateHome, ok := lookupEnv("XDG_STATE_HOME"); ok && stateHome != "" {
		if !filepath.IsAbs(stateHome) {
			return "", fmt.Errorf("XDG_STATE_HOME %q is relative", stateHome)
		}
		return filepath.Join(stateHome, networkPreferencesStateDirName, networkPreferencesStateFileName), nil
	}

	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	if home == "" {
		return "", errors.New("user home dir is empty")
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("user home dir %q is relative", home)
	}
	return filepath.Join(home, ".local", "state", networkPreferencesStateDirName, networkPreferencesStateFileName), nil
}

func (p *NetworkPreferences) SaveMode(modemID string, mode ModemModePair) error {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return errors.New("modem id is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	store, err := readNetworkPreferencesState(p.path)
	if err != nil {
		return err
	}
	entry := store.Modems[modemID]
	entry.Mode = &networkPreferenceMode{
		Allowed:   mode.Allowed,
		Preferred: mode.Preferred,
	}
	store.Modems[modemID] = entry
	return writeNetworkPreferencesState(p.path, store)
}

func (p *NetworkPreferences) SaveBands(modemID string, bands []ModemBand) error {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return errors.New("modem id is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	store, err := readNetworkPreferencesState(p.path)
	if err != nil {
		return err
	}
	entry := store.Modems[modemID]
	entry.Bands = slices.Clone(bands)
	store.Modems[modemID] = entry
	return writeNetworkPreferencesState(p.path, store)
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
	prefs, ok, err := p.loadForModem(m.EquipmentIdentifier)
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

func (p *NetworkPreferences) loadForModem(modemID string) (savedNetworkPreferences, bool, error) {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return savedNetworkPreferences{}, false, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	store, err := readNetworkPreferencesState(p.path)
	if err != nil {
		return savedNetworkPreferences{}, false, err
	}
	entry, ok := store.Modems[modemID]
	if !ok {
		return savedNetworkPreferences{}, false, nil
	}
	return entry, true, nil
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

func readNetworkPreferencesState(path string) (networkPreferencesStateFile, error) {
	store := networkPreferencesStateFile{
		Version: networkPreferencesStateVersion,
		Modems:  make(map[string]savedNetworkPreferences),
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, fmt.Errorf("read network preferences state: %w", err)
	}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return networkPreferencesStateFile{}, fmt.Errorf("decode network preferences state: %w", err)
	}
	if store.Version != networkPreferencesStateVersion {
		return networkPreferencesStateFile{}, fmt.Errorf("network preferences state version %d is unsupported", store.Version)
	}
	if store.Modems == nil {
		store.Modems = make(map[string]savedNetworkPreferences)
	}
	return store, nil
}

func writeNetworkPreferencesState(path string, store networkPreferencesStateFile) error {
	store.Version = networkPreferencesStateVersion
	if store.Modems == nil {
		store.Modems = make(map[string]savedNetworkPreferences)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode network preferences state: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create network preferences state directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure network preferences state directory: %w", err)
	}
	tempFile, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create network preferences state temp file: %w", err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write network preferences state temp file: %w", err)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("secure network preferences state temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close network preferences state temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace network preferences state file: %w", err)
	}
	removeTemp = false
	return nil
}
