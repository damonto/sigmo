package internet

import "github.com/damonto/sigmo/internal/pkg/netlink"

type fileConnectionState struct {
	proxyPath string
	routePath string
}

func (s fileConnectionState) saveProxyStateForModem(modemID string, interfaceName string) error {
	return saveProxyStateForModem(s.proxyPath, modemID, interfaceName)
}

func (s fileConnectionState) loadProxyStateForModem(modemID string, interfaceName string) (bool, bool, error) {
	return loadProxyStateForModem(s.proxyPath, modemID, interfaceName)
}

func (s fileConnectionState) deleteProxyState(interfaceName string) error {
	return deleteProxyState(s.proxyPath, interfaceName)
}

func (s fileConnectionState) proxyInterfacesForModem(modemID string) ([]string, error) {
	return proxyInterfacesForModem(s.proxyPath, modemID)
}

func (s fileConnectionState) saveRouteStateForModem(modemID string, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error {
	return saveRouteStateForModem(s.routePath, modemID, interfaceName, preferred, changes)
}

func (s fileConnectionState) putRouteStateForModem(modemID string, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error {
	return putRouteStateForModem(s.routePath, modemID, interfaceName, preferred, changes)
}

func (s fileConnectionState) loadRouteStateForModem(modemID string, interfaceName string) ([]defaultRouteChange, bool, error) {
	return loadRouteStateForModem(s.routePath, modemID, interfaceName)
}

func (s fileConnectionState) loadAllRouteStates() (map[string]savedRouteState, error) {
	return loadAllRouteStates(s.routePath)
}

func (s fileConnectionState) deleteRouteState(interfaceName string) error {
	return deleteRouteState(s.routePath, interfaceName)
}

func takeoverDefaultRoutesWithState(path string, modemID string, interfaceName string, preferred []netlink.DefaultRoute, ops defaultRouteOps) ([]defaultRouteChange, error) {
	return takeoverDefaultRoutesWithStore(fileConnectionState{routePath: path}, modemID, interfaceName, preferred, ops)
}

func cleanupDefaultRouteChanges(path string, interfaceName string, changes []defaultRouteChange, ops defaultRouteOps) error {
	return cleanupDefaultRouteChangesWithStore(fileConnectionState{routePath: path}, interfaceName, changes, ops)
}

func restoreStaleDefaultRouteStatesWithState(path string, target routeStateRestoreTarget, ops defaultRouteOps) error {
	return restoreStaleDefaultRouteStatesWithStore(fileConnectionState{routePath: path}, target, ops)
}
