package internet

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/netlink"
)

const routeStateVersion = 1

type routeStateFile struct {
	Version    int                        `json:"version"`
	Interfaces map[string]routeStateEntry `json:"interfaces"`
}

func saveRouteStateForModem(path string, modemID string, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error {
	if interfaceName == "" {
		return errors.New("interface name is empty")
	}
	store, err := readRouteState(path)
	if err != nil {
		return err
	}
	if _, ok := store.Interfaces[interfaceName]; ok {
		return fmt.Errorf("route state for interface %s already exists", interfaceName)
	}
	store.Interfaces[interfaceName] = routeStateEntry{
		Modem:     modemID,
		Preferred: routeStateRoutes(preferred),
		Changes:   routeStateChanges(changes),
	}
	return writeRouteState(path, store)
}

func putRouteStateForModem(path string, modemID string, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error {
	if interfaceName == "" {
		return errors.New("interface name is empty")
	}
	store, err := readRouteState(path)
	if err != nil {
		return err
	}
	store.Interfaces[interfaceName] = routeStateEntry{
		Modem:     modemID,
		Preferred: routeStateRoutes(preferred),
		Changes:   routeStateChanges(changes),
	}
	return writeRouteState(path, store)
}

func loadRouteStateForModem(path string, modemID string, interfaceName string) ([]defaultRouteChange, bool, error) {
	return loadRouteStateMatching(path, modemID, interfaceName, true)
}

func loadRouteStateMatching(path string, modemID string, interfaceName string, scoped bool) ([]defaultRouteChange, bool, error) {
	store, err := readRouteState(path)
	if err != nil {
		return nil, false, err
	}
	entry, ok := store.Interfaces[interfaceName]
	if !ok {
		return nil, false, nil
	}
	if scoped {
		owner := strings.TrimSpace(entry.Modem)
		if owner != "" && owner != strings.TrimSpace(modemID) {
			return nil, false, nil
		}
	}
	changes, err := defaultRouteChangesFromState(entry.Changes)
	if err != nil {
		return nil, false, err
	}
	return changes, true, nil
}

func loadAllRouteStates(path string) (map[string]savedRouteState, error) {
	store, err := readRouteState(path)
	if err != nil {
		return nil, err
	}
	result := make(map[string]savedRouteState, len(store.Interfaces))
	for interfaceName, entry := range store.Interfaces {
		preferred, err := defaultRoutesFromState(entry.Preferred)
		if err != nil {
			return nil, err
		}
		changes, err := defaultRouteChangesFromState(entry.Changes)
		if err != nil {
			return nil, err
		}
		result[interfaceName] = savedRouteState{
			ModemID:   entry.Modem,
			Preferred: preferred,
			Changes:   changes,
		}
	}
	return result, nil
}

func deleteRouteState(path string, interfaceName string) error {
	store, err := readRouteState(path)
	if err != nil {
		return err
	}
	if _, ok := store.Interfaces[interfaceName]; !ok {
		return nil
	}
	delete(store.Interfaces, interfaceName)
	if len(store.Interfaces) == 0 {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return writeRouteState(path, store)
}

func readRouteState(path string) (routeStateFile, error) {
	store := routeStateFile{
		Version:    routeStateVersion,
		Interfaces: make(map[string]routeStateEntry),
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, fmt.Errorf("read route state: %w", err)
	}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return routeStateFile{}, fmt.Errorf("decode route state: %w", err)
	}
	if store.Version != routeStateVersion {
		return routeStateFile{}, fmt.Errorf("route state version %d is unsupported", store.Version)
	}
	if store.Interfaces == nil {
		store.Interfaces = make(map[string]routeStateEntry)
	}
	return store, nil
}

func writeRouteState(path string, store routeStateFile) error {
	store.Version = routeStateVersion
	if store.Interfaces == nil {
		store.Interfaces = make(map[string]routeStateEntry)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode route state: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create route state directory: %w", err)
	}
	tempPath := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("write route state temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace route state file: %w", err)
	}
	return nil
}
