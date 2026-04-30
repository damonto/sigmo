package internet

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/netlink"
)

const (
	defaultRouteStatePath = "/run/sigmo/internet-routes.json"
	routeStateVersion     = 1
)

type routeStateFile struct {
	Version    int                        `json:"version"`
	Interfaces map[string]routeStateEntry `json:"interfaces"`
}

type routeStateEntry struct {
	Modem     string             `json:"modem,omitempty"`
	Preferred []routeStateRoute  `json:"preferred"`
	Changes   []routeStateChange `json:"changes"`
}

type routeStateChange struct {
	Original    routeStateRoute `json:"original"`
	Replacement routeStateRoute `json:"replacement"`
}

type routeStateRoute struct {
	Interface string `json:"interface"`
	Family    int    `json:"family"`
	Protocol  int    `json:"protocol"`
	Scope     int    `json:"scope"`
	Gateway   string `json:"gateway,omitempty"`
	Metric    int    `json:"metric"`
}

type savedRouteState struct {
	ModemID   string
	Preferred []netlink.DefaultRoute
	Changes   []defaultRouteChange
}

func loadDefaultRouteStateForModem(modemID string, interfaceName string) ([]defaultRouteChange, bool, error) {
	return loadRouteStateForModem(defaultRouteStatePath, modemID, interfaceName)
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

func routeStateRoutes(routes []netlink.DefaultRoute) []routeStateRoute {
	result := make([]routeStateRoute, 0, len(routes))
	for _, route := range routes {
		result = append(result, routeStateRouteFromDefault(route))
	}
	return result
}

func routeStateChanges(changes []defaultRouteChange) []routeStateChange {
	result := make([]routeStateChange, 0, len(changes))
	for _, change := range changes {
		result = append(result, routeStateChange{
			Original:    routeStateRouteFromDefault(change.Original),
			Replacement: routeStateRouteFromDefault(change.Replacement),
		})
	}
	return result
}

func defaultRoutesFromState(routes []routeStateRoute) ([]netlink.DefaultRoute, error) {
	result := make([]netlink.DefaultRoute, 0, len(routes))
	for _, route := range routes {
		defaultRoute, err := defaultRouteFromState(route)
		if err != nil {
			return nil, err
		}
		result = append(result, defaultRoute)
	}
	return result, nil
}

func defaultRouteChangesFromState(changes []routeStateChange) ([]defaultRouteChange, error) {
	result := make([]defaultRouteChange, 0, len(changes))
	for _, change := range changes {
		original, err := defaultRouteFromState(change.Original)
		if err != nil {
			return nil, err
		}
		replacement, err := defaultRouteFromState(change.Replacement)
		if err != nil {
			return nil, err
		}
		result = append(result, defaultRouteChange{
			Original:    original,
			Replacement: replacement,
		})
	}
	return result, nil
}

func routeStateRouteFromDefault(route netlink.DefaultRoute) routeStateRoute {
	state := routeStateRoute{
		Interface: route.Interface,
		Family:    route.Family,
		Protocol:  route.Protocol,
		Scope:     route.Scope,
		Metric:    route.Metric,
	}
	if route.Gateway.IsValid() {
		state.Gateway = route.Gateway.String()
	}
	return state
}

func defaultRouteFromState(state routeStateRoute) (netlink.DefaultRoute, error) {
	route := netlink.DefaultRoute{
		Interface: state.Interface,
		Family:    state.Family,
		Protocol:  state.Protocol,
		Scope:     state.Scope,
		Metric:    state.Metric,
	}
	if state.Gateway == "" {
		return route, nil
	}
	gateway, err := netip.ParseAddr(state.Gateway)
	if err != nil {
		return netlink.DefaultRoute{}, fmt.Errorf("parse route state gateway: %w", err)
	}
	route.Gateway = gateway
	return route, nil
}
