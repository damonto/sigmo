package networkprefs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"

	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const preferencesKey = "network.preferences"

var retryInterval = 5 * time.Second

type Store struct {
	storage *storage.Store
	mu      sync.Mutex
}

type savedPreferences struct {
	Mode         *wwanmodem.Mode   `json:"mode,omitempty"`
	Bands        *[]wwanmodem.Band `json:"bands,omitempty"`
	AirplaneMode *bool             `json:"airplaneMode,omitempty"`
}

func New(storage *storage.Store) (*Store, error) {
	if storage == nil {
		return nil, errors.New("network preferences storage is required")
	}
	return &Store{storage: storage}, nil
}

func (s *Store) SaveMode(ctx context.Context, modemID string, mode wwanmodem.Mode) error {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return errors.New("modem id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, _, err := s.loadLocked(ctx, modemID)
	if err != nil {
		return err
	}
	entry.Mode = &mode
	return s.saveLocked(ctx, modemID, entry)
}

func (s *Store) SaveBands(ctx context.Context, modemID string, bands []wwanmodem.Band) error {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return errors.New("modem id is required")
	}
	if len(bands) == 0 {
		return errors.New("bands are required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, _, err := s.loadLocked(ctx, modemID)
	if err != nil {
		return err
	}
	cloned := make([]wwanmodem.Band, len(bands))
	copy(cloned, bands)
	entry.Bands = &cloned
	return s.saveLocked(ctx, modemID, entry)
}

func (s *Store) SaveAirplaneMode(ctx context.Context, modemID string, enabled bool) error {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return errors.New("modem id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, _, err := s.loadLocked(ctx, modemID)
	if err != nil {
		return err
	}
	entry.AirplaneMode = &enabled
	return s.saveLocked(ctx, modemID, entry)
}

func (s *Store) SavedAirplaneMode(ctx context.Context, modemID string) (bool, bool, error) {
	prefs, ok, err := s.load(ctx, modemID)
	if err != nil || !ok || prefs.AirplaneMode == nil {
		return false, false, err
	}
	return *prefs.AirplaneMode, true, nil
}

// SkipEnableDisabledInAirplaneMode keeps automatic modem enabling from turning RF back on.
func SkipEnableDisabledInAirplaneMode(preferences *Store) func(context.Context, *modem.Modem) (bool, error) {
	return func(ctx context.Context, m *modem.Modem) (bool, error) {
		if preferences == nil || m == nil {
			return false, nil
		}
		enabled, ok, err := preferences.SavedAirplaneMode(ctx, m.EquipmentIdentifier)
		if err != nil {
			return false, fmt.Errorf("read airplane mode preference: %w", err)
		}
		return ok && enabled, nil
	}
}

// Restore reapplies saved radio preferences until the modem is ready or ctx is canceled.
func (s *Store) Restore(ctx context.Context, m *modem.Modem) {
	warned := false
	for {
		retry, err := s.restoreOnce(ctx, m)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		if !retry {
			slog.Warn("restore network preferences", "imei", m.EquipmentIdentifier, "error", err)
			return
		}
		if warned {
			slog.Debug("retry network preferences restore", "imei", m.EquipmentIdentifier, "error", err)
		} else {
			slog.Warn("restore network preferences", "imei", m.EquipmentIdentifier, "error", err)
			warned = true
		}
		if err := sleepContext(ctx, retryInterval); err != nil {
			return
		}
	}
}

func (s *Store) restoreOnce(ctx context.Context, m *modem.Modem) (bool, error) {
	prefs, ok, err := s.load(ctx, m.EquipmentIdentifier)
	if err != nil {
		return false, fmt.Errorf("load network preferences: %w", err)
	}
	if !ok {
		return false, nil
	}

	if prefs.AirplaneMode != nil {
		retry, err := restoreAirplaneMode(ctx, m, *prefs.AirplaneMode)
		if err != nil {
			return retry, err
		}
		if *prefs.AirplaneMode {
			return false, nil
		}
	}

	var result error
	retry := false
	if prefs.Mode != nil {
		nextRetry, err := restoreMode(ctx, m, *prefs.Mode)
		if err != nil {
			result = errors.Join(result, err)
			retry = retry || nextRetry
		}
	}
	if prefs.Bands != nil {
		nextRetry, err := restoreBands(ctx, m, *prefs.Bands)
		if err != nil {
			result = errors.Join(result, err)
			retry = retry || nextRetry
		}
	}
	return retry, result
}

func (s *Store) load(ctx context.Context, modemID string) (savedPreferences, bool, error) {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return savedPreferences{}, false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadLocked(ctx, modemID)
}

func (s *Store) loadLocked(ctx context.Context, modemID string) (savedPreferences, bool, error) {
	var entry savedPreferences
	err := s.storage.Get(ctx, "modem:"+modemID, preferencesKey, &entry)
	if errors.Is(err, storage.ErrNotFound) {
		return savedPreferences{}, false, nil
	}
	if err != nil {
		return savedPreferences{}, false, err
	}
	return entry, true, nil
}

func (s *Store) saveLocked(ctx context.Context, modemID string, entry savedPreferences) error {
	return s.storage.Put(ctx, "modem:"+modemID, preferencesKey, entry)
}

func restoreAirplaneMode(ctx context.Context, m *modem.Modem, enabled bool) (bool, error) {
	current, err := m.AirplaneMode(ctx)
	if errors.Is(err, wwanmodem.ErrNotSupported) {
		return false, err
	}
	if err != nil {
		return false, fmt.Errorf("read airplane mode: %w", err)
	}
	if current == enabled {
		return false, nil
	}
	if err := m.SetAirplaneMode(ctx, enabled); err != nil {
		return false, fmt.Errorf("set airplane mode: %w", err)
	}
	slog.Info("airplane mode restored", "imei", m.EquipmentIdentifier, "enabled", enabled)
	return false, nil
}

func restoreMode(ctx context.Context, m *modem.Modem, mode wwanmodem.Mode) (bool, error) {
	supported, current, err := m.Modes(ctx)
	if err != nil {
		return modem.IsTransientRestartError(err), fmt.Errorf("read modes: %w", err)
	}
	if !slices.Contains(supported, mode) {
		return false, fmt.Errorf("saved mode unsupported: allowed=%d preferred=%d", mode.Allowed, mode.Preferred)
	}
	if current == mode {
		return false, nil
	}
	if err := m.SetCurrentModes(ctx, mode); err != nil {
		return modem.IsTransientRestartError(err), fmt.Errorf("set current modes: %w", err)
	}
	slog.Info("network mode restored", "imei", m.EquipmentIdentifier, "allowed", mode.Allowed, "preferred", mode.Preferred)
	return false, nil
}

func restoreBands(ctx context.Context, m *modem.Modem, bands []wwanmodem.Band) (bool, error) {
	if len(bands) == 0 {
		return false, errors.New("saved bands are empty")
	}
	if duplicateBand(bands) {
		return false, errors.New("saved bands contain duplicates")
	}

	supported, err := m.SupportedBands(ctx)
	if err != nil {
		return modem.IsTransientRestartError(err), fmt.Errorf("read supported bands: %w", err)
	}
	for _, band := range bands {
		if !slices.Contains(supported, band) {
			return false, fmt.Errorf("saved band unsupported: technology=%d number=%d", band.Technology, band.Number)
		}
	}

	current, err := m.CurrentBands(ctx)
	if err != nil {
		return modem.IsTransientRestartError(err), fmt.Errorf("read current bands: %w", err)
	}
	if sameBands(current, bands) {
		return false, nil
	}
	if err := m.SetCurrentBands(ctx, bands); err != nil {
		return modem.IsTransientRestartError(err), fmt.Errorf("set current bands: %w", err)
	}
	slog.Info("network bands restored", "imei", m.EquipmentIdentifier, "bands", bands)
	return false, nil
}

func sameBands(a, b []wwanmodem.Band) bool {
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

func duplicateBand(bands []wwanmodem.Band) bool {
	seen := make(map[wwanmodem.Band]struct{}, len(bands))
	for _, band := range bands {
		if _, ok := seen[band]; ok {
			return true
		}
		seen[band] = struct{}{}
	}
	return false
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
