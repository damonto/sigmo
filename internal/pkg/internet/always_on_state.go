package internet

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	alwaysOnStatePath    = "/run/sigmo/internet-always-on.json"
	alwaysOnStateVersion = 1
)

type alwaysOnStateFile struct {
	Version int                           `json:"version"`
	Modems  map[string]alwaysOnStateEntry `json:"modems"`
}

type alwaysOnStateEntry struct {
	APN          string `json:"apn"`
	IPType       string `json:"ipType,omitempty"`
	APNUsername  string `json:"apnUsername,omitempty"`
	APNPassword  string `json:"apnPassword,omitempty"`
	APNAuth      string `json:"apnAuth,omitempty"`
	DefaultRoute bool   `json:"defaultRoute"`
	ProxyEnabled bool   `json:"proxyEnabled"`
	AlwaysOn     bool   `json:"alwaysOn"`
}

func loadAlwaysOnStates(path string) (map[string]Preferences, error) {
	store, err := readAlwaysOnState(path)
	if err != nil {
		return nil, err
	}
	result := make(map[string]Preferences, len(store.Modems))
	for modemID, entry := range store.Modems {
		modemID = strings.TrimSpace(modemID)
		if modemID == "" || !entry.AlwaysOn {
			continue
		}
		result[modemID] = preferencesFromAlwaysOnState(entry)
	}
	return result, nil
}

func loadAlwaysOnStateForModem(path string, modemID string) (Preferences, bool, error) {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return Preferences{}, false, nil
	}
	store, err := readAlwaysOnState(path)
	if err != nil {
		return Preferences{}, false, err
	}
	entry, ok := store.Modems[modemID]
	if !ok || !entry.AlwaysOn {
		return Preferences{}, false, nil
	}
	return preferencesFromAlwaysOnState(entry), true, nil
}

func saveAlwaysOnStateForModem(path string, modemID string, prefs Preferences) error {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return errors.New("modem id is required")
	}
	if !prefs.AlwaysOn {
		return deleteAlwaysOnStateForModem(path, modemID)
	}
	store, err := readAlwaysOnState(path)
	if err != nil {
		return err
	}
	store.Modems[modemID] = alwaysOnStateEntry{
		APN:          strings.TrimSpace(prefs.APN),
		IPType:       strings.ToLower(strings.TrimSpace(prefs.IPType)),
		APNUsername:  strings.TrimSpace(prefs.APNUsername),
		APNPassword:  prefs.APNPassword,
		APNAuth:      strings.ToLower(strings.TrimSpace(prefs.APNAuth)),
		DefaultRoute: prefs.DefaultRoute,
		ProxyEnabled: prefs.ProxyEnabled,
		AlwaysOn:     true,
	}
	return writeAlwaysOnState(path, store)
}

func deleteAlwaysOnStateForModem(path string, modemID string) error {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return nil
	}
	store, err := readAlwaysOnState(path)
	if err != nil {
		return err
	}
	if _, ok := store.Modems[modemID]; !ok {
		return nil
	}
	delete(store.Modems, modemID)
	if len(store.Modems) == 0 {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return writeAlwaysOnState(path, store)
}

func readAlwaysOnState(path string) (alwaysOnStateFile, error) {
	store := alwaysOnStateFile{
		Version: alwaysOnStateVersion,
		Modems:  make(map[string]alwaysOnStateEntry),
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, fmt.Errorf("read always on state: %w", err)
	}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return alwaysOnStateFile{}, fmt.Errorf("decode always on state: %w", err)
	}
	if store.Version != alwaysOnStateVersion {
		return alwaysOnStateFile{}, fmt.Errorf("always on state version %d is unsupported", store.Version)
	}
	if store.Modems == nil {
		store.Modems = make(map[string]alwaysOnStateEntry)
	}
	return store, nil
}

func writeAlwaysOnState(path string, store alwaysOnStateFile) error {
	store.Version = alwaysOnStateVersion
	if store.Modems == nil {
		store.Modems = make(map[string]alwaysOnStateEntry)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode always on state: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create always on state directory: %w", err)
	}
	tempPath := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("write always on state temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace always on state file: %w", err)
	}
	return nil
}

func preferencesFromAlwaysOnState(entry alwaysOnStateEntry) Preferences {
	return Preferences{
		APN:          strings.TrimSpace(entry.APN),
		IPType:       strings.ToLower(strings.TrimSpace(entry.IPType)),
		APNUsername:  strings.TrimSpace(entry.APNUsername),
		APNPassword:  entry.APNPassword,
		APNAuth:      strings.ToLower(strings.TrimSpace(entry.APNAuth)),
		DefaultRoute: entry.DefaultRoute,
		ProxyEnabled: entry.ProxyEnabled,
		AlwaysOn:     true,
	}
}
