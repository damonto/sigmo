package internet

import (
	"context"

	"github.com/damonto/sigmo/internal/pkg/netlink"
)

type fileConnectionState struct {
	proxyPath string
	routePath string
}

func (s fileConnectionState) saveProxyStateForModem(_ context.Context, modemID string, interfaceName string) error {
	return saveProxyStateForModem(s.proxyPath, modemID, interfaceName)
}

func (s fileConnectionState) loadProxyStateForModem(_ context.Context, modemID string, interfaceName string) (bool, bool, error) {
	return loadProxyStateForModem(s.proxyPath, modemID, interfaceName)
}

func (s fileConnectionState) deleteProxyState(_ context.Context, interfaceName string) error {
	return deleteProxyState(s.proxyPath, interfaceName)
}

func (s fileConnectionState) proxyInterfacesForModem(_ context.Context, modemID string) ([]string, error) {
	return proxyInterfacesForModem(s.proxyPath, modemID)
}

func (s fileConnectionState) saveRouteStateForModem(_ context.Context, modemID string, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error {
	return saveRouteStateForModem(s.routePath, modemID, interfaceName, preferred, changes)
}

func (s fileConnectionState) putRouteStateForModem(_ context.Context, modemID string, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error {
	return putRouteStateForModem(s.routePath, modemID, interfaceName, preferred, changes)
}

func (s fileConnectionState) loadRouteStateForModem(_ context.Context, modemID string, interfaceName string) ([]defaultRouteChange, bool, error) {
	return loadRouteStateForModem(s.routePath, modemID, interfaceName)
}

func (s fileConnectionState) loadAllRouteStates(_ context.Context) (map[string]savedRouteState, error) {
	return loadAllRouteStates(s.routePath)
}

func (s fileConnectionState) deleteRouteState(_ context.Context, interfaceName string) error {
	return deleteRouteState(s.routePath, interfaceName)
}

func takeoverDefaultRoutesWithState(path string, modemID string, interfaceName string, preferred []netlink.DefaultRoute, ops defaultRouteOps) ([]defaultRouteChange, error) {
	return takeoverDefaultRoutesWithStore(context.Background(), fileConnectionState{routePath: path}, modemID, interfaceName, preferred, ops)
}

func cleanupDefaultRouteChanges(path string, interfaceName string, changes []defaultRouteChange, ops defaultRouteOps) error {
	return cleanupDefaultRouteChangesWithStore(context.Background(), fileConnectionState{routePath: path}, interfaceName, changes, ops)
}

func restoreStaleDefaultRouteStatesWithState(path string, target routeStateRestoreTarget, ops defaultRouteOps) error {
	return restoreStaleDefaultRouteStatesWithStore(context.Background(), fileConnectionState{routePath: path}, target, ops)
}
