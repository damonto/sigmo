package internet

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	proxyStatePath    = "/run/sigmo/internet-proxies.json"
	proxyStateVersion = 1
)

type proxyStateFile struct {
	Version    int                        `json:"version"`
	Interfaces map[string]proxyStateEntry `json:"interfaces"`
}

type proxyStateEntry struct {
	Modem string `json:"modem,omitempty"`
}

func saveProxyStateForModem(path string, modemID string, interfaceName string) error {
	modemID = strings.TrimSpace(modemID)
	interfaceName = strings.TrimSpace(interfaceName)
	if interfaceName == "" {
		return errors.New("interface name is empty")
	}
	store, err := readProxyState(path)
	if err != nil {
		return err
	}
	entry := store.Interfaces[interfaceName]
	owner := strings.TrimSpace(entry.Modem)
	if owner != "" && owner != modemID {
		return fmt.Errorf("proxy state for interface %s belongs to modem %s", interfaceName, owner)
	}
	if modemID != "" {
		for name, saved := range store.Interfaces {
			if name != interfaceName && strings.TrimSpace(saved.Modem) == modemID {
				delete(store.Interfaces, name)
			}
		}
	}
	entry.Modem = modemID
	store.Interfaces[interfaceName] = entry
	return writeProxyState(path, store)
}

func loadProxyStateForModem(path string, modemID string, interfaceName string) (bool, bool, error) {
	modemID = strings.TrimSpace(modemID)
	interfaceName = strings.TrimSpace(interfaceName)
	store, err := readProxyState(path)
	if err != nil {
		return false, false, err
	}
	entry, ok := store.Interfaces[interfaceName]
	if !ok {
		return false, false, nil
	}
	owner := strings.TrimSpace(entry.Modem)
	if owner != "" && owner != modemID {
		return false, false, nil
	}
	return true, true, nil
}

func deleteProxyState(path string, interfaceName string) error {
	interfaceName = strings.TrimSpace(interfaceName)
	store, err := readProxyState(path)
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
	return writeProxyState(path, store)
}

func proxyInterfacesForModem(path string, modemID string) ([]string, error) {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return nil, nil
	}
	store, err := readProxyState(path)
	if err != nil {
		return nil, err
	}
	var interfaces []string
	for interfaceName, entry := range store.Interfaces {
		if strings.TrimSpace(entry.Modem) == modemID {
			interfaces = append(interfaces, interfaceName)
		}
	}
	slices.Sort(interfaces)
	return interfaces, nil
}

func readProxyState(path string) (proxyStateFile, error) {
	store := proxyStateFile{
		Version:    proxyStateVersion,
		Interfaces: make(map[string]proxyStateEntry),
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, fmt.Errorf("read proxy state: %w", err)
	}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return proxyStateFile{}, fmt.Errorf("decode proxy state: %w", err)
	}
	if store.Version != proxyStateVersion {
		return proxyStateFile{}, fmt.Errorf("proxy state version %d is unsupported", store.Version)
	}
	if store.Interfaces == nil {
		store.Interfaces = make(map[string]proxyStateEntry)
	}
	return store, nil
}

func writeProxyState(path string, store proxyStateFile) error {
	store.Version = proxyStateVersion
	if store.Interfaces == nil {
		store.Interfaces = make(map[string]proxyStateEntry)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode proxy state: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create proxy state directory: %w", err)
	}
	tempPath := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("write proxy state temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace proxy state file: %w", err)
	}
	return nil
}
