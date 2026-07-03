package internet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	interfaceScopePrefix = "interface:"
	proxyKVKey           = "internet.proxy"
	routeKVKey           = "internet.route"
)

type connectionStateStore interface {
	saveProxyStateForModem(ctx context.Context, modemID string, interfaceName string) error
	loadProxyStateForModem(ctx context.Context, modemID string, interfaceName string) (bool, bool, error)
	deleteProxyState(ctx context.Context, interfaceName string) error
	proxyInterfacesForModem(ctx context.Context, modemID string) ([]string, error)
	saveRouteStateForModem(ctx context.Context, modemID string, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error
	putRouteStateForModem(ctx context.Context, modemID string, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error
	loadRouteStateForModem(ctx context.Context, modemID string, interfaceName string) ([]defaultRouteChange, bool, error)
	loadAllRouteStates(ctx context.Context) (map[string]savedRouteState, error)
	deleteRouteState(ctx context.Context, interfaceName string) error
}

type dbConnectionState struct {
	store *storage.Store
}

type proxyStateEntry struct {
	Modem string `json:"modem,omitempty"`
}

func (s dbConnectionState) saveProxyStateForModem(ctx context.Context, modemID string, interfaceName string) error {
	modemID = strings.TrimSpace(modemID)
	interfaceName = strings.TrimSpace(interfaceName)
	if interfaceName == "" {
		return errors.New("interface name is empty")
	}
	entry, ok, err := s.proxyState(ctx, interfaceName)
	if err != nil {
		return err
	}
	owner := strings.TrimSpace(entry.Modem)
	if ok && owner != "" && owner != modemID {
		return fmt.Errorf("proxy state for interface %s belongs to modem %s", interfaceName, owner)
	}
	if modemID != "" {
		interfaces, err := s.proxyInterfacesForModem(ctx, modemID)
		if err != nil {
			return err
		}
		for _, name := range interfaces {
			if name != interfaceName {
				if err := s.deleteProxyState(ctx, name); err != nil {
					return err
				}
			}
		}
	}
	entry.Modem = modemID
	return s.store.Put(ctx, interfaceScope(interfaceName), proxyKVKey, entry)
}

func (s dbConnectionState) loadProxyStateForModem(ctx context.Context, modemID string, interfaceName string) (bool, bool, error) {
	modemID = strings.TrimSpace(modemID)
	entry, ok, err := s.proxyState(ctx, interfaceName)
	if err != nil || !ok {
		return false, false, err
	}
	owner := strings.TrimSpace(entry.Modem)
	if owner != "" && owner != modemID {
		return false, false, nil
	}
	return true, true, nil
}

func (s dbConnectionState) deleteProxyState(ctx context.Context, interfaceName string) error {
	return s.store.Delete(ctx, interfaceScope(interfaceName), proxyKVKey)
}

func (s dbConnectionState) proxyInterfacesForModem(ctx context.Context, modemID string) ([]string, error) {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return nil, nil
	}
	raw, err := s.store.ListRaw(ctx, interfaceScopePrefix, proxyKVKey)
	if err != nil {
		return nil, err
	}
	var interfaces []string
	for scope, value := range raw {
		var entry proxyStateEntry
		if err := json.Unmarshal([]byte(value), &entry); err != nil {
			return nil, fmt.Errorf("decode proxy state for %s: %w", scope, err)
		}
		if strings.TrimSpace(entry.Modem) == modemID {
			interfaces = append(interfaces, strings.TrimPrefix(scope, interfaceScopePrefix))
		}
	}
	slices.Sort(interfaces)
	return interfaces, nil
}

func (s dbConnectionState) saveRouteStateForModem(ctx context.Context, modemID string, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error {
	interfaceName = strings.TrimSpace(interfaceName)
	if interfaceName == "" {
		return errors.New("interface name is empty")
	}
	_, ok, err := s.routeState(ctx, interfaceName)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("route state for interface %s already exists", interfaceName)
	}
	return s.putRouteStateForModem(ctx, modemID, interfaceName, preferred, changes)
}

func (s dbConnectionState) putRouteStateForModem(ctx context.Context, modemID string, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error {
	interfaceName = strings.TrimSpace(interfaceName)
	if interfaceName == "" {
		return errors.New("interface name is empty")
	}
	entry := routeStateEntry{
		Modem:     modemID,
		Preferred: preferred,
		Changes:   changes,
	}
	return s.store.Put(ctx, interfaceScope(interfaceName), routeKVKey, entry)
}

func (s dbConnectionState) loadRouteStateForModem(ctx context.Context, modemID string, interfaceName string) ([]defaultRouteChange, bool, error) {
	entry, ok, err := s.routeState(ctx, interfaceName)
	if err != nil || !ok {
		return nil, false, err
	}
	owner := strings.TrimSpace(entry.Modem)
	if owner != "" && owner != strings.TrimSpace(modemID) {
		return nil, false, nil
	}
	return entry.Changes, true, nil
}

func (s dbConnectionState) loadAllRouteStates(ctx context.Context) (map[string]savedRouteState, error) {
	raw, err := s.store.ListRaw(ctx, interfaceScopePrefix, routeKVKey)
	if err != nil {
		return nil, err
	}
	result := make(map[string]savedRouteState, len(raw))
	for scope, value := range raw {
		var entry routeStateEntry
		if err := json.Unmarshal([]byte(value), &entry); err != nil {
			return nil, fmt.Errorf("decode route state for %s: %w", scope, err)
		}
		result[strings.TrimPrefix(scope, interfaceScopePrefix)] = savedRouteState{
			ModemID:   entry.Modem,
			Preferred: entry.Preferred,
			Changes:   entry.Changes,
		}
	}
	return result, nil
}

func (s dbConnectionState) deleteRouteState(ctx context.Context, interfaceName string) error {
	return s.store.Delete(ctx, interfaceScope(interfaceName), routeKVKey)
}

func (s dbConnectionState) proxyState(ctx context.Context, interfaceName string) (proxyStateEntry, bool, error) {
	var entry proxyStateEntry
	err := s.store.Get(ctx, interfaceScope(interfaceName), proxyKVKey, &entry)
	if errors.Is(err, storage.ErrNotFound) {
		return proxyStateEntry{}, false, nil
	}
	if err != nil {
		return proxyStateEntry{}, false, err
	}
	return entry, true, nil
}

func (s dbConnectionState) routeState(ctx context.Context, interfaceName string) (routeStateEntry, bool, error) {
	var entry routeStateEntry
	err := s.store.Get(ctx, interfaceScope(interfaceName), routeKVKey, &entry)
	if errors.Is(err, storage.ErrNotFound) {
		return routeStateEntry{}, false, nil
	}
	if err != nil {
		return routeStateEntry{}, false, err
	}
	return entry, true, nil
}

func interfaceScope(interfaceName string) string {
	return interfaceScopePrefix + strings.TrimSpace(interfaceName)
}
