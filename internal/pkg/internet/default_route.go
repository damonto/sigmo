package internet

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/damonto/sigmo/internal/pkg/netlink"
)

const (
	defaultRouteMetric   = 10
	secondaryRouteMetric = 9000
)

type defaultRouteChange struct {
	Original    netlink.DefaultRoute
	Replacement netlink.DefaultRoute
}

type defaultRouteOps struct {
	defaultRoutes      func() ([]netlink.DefaultRoute, error)
	addDefaultRoute    func(netlink.DefaultRoute) error
	deleteDefaultRoute func(netlink.DefaultRoute) error
}

type routeStateRestoreTarget struct {
	modemID        string
	interfaceNames []string
}

var netlinkDefaultRouteOps = defaultRouteOps{
	defaultRoutes:      netlink.DefaultRoutes,
	addDefaultRoute:    netlink.AddDefaultRoute,
	deleteDefaultRoute: netlink.DeleteDefaultRoute,
}

func takeoverDefaultRoutes(modemID string, interfaceName string, preferred []netlink.DefaultRoute) ([]defaultRouteChange, error) {
	return takeoverDefaultRoutesWithState(defaultRouteStatePath, modemID, interfaceName, preferred, netlinkDefaultRouteOps)
}

func takeoverDefaultRoutesWithState(path string, modemID string, interfaceName string, preferred []netlink.DefaultRoute, ops defaultRouteOps) ([]defaultRouteChange, error) {
	current, err := ops.defaultRoutes()
	if err != nil {
		return nil, err
	}

	changes := defaultRouteChanges(current, preferred)
	logDefaultRouteTakeover(modemID, interfaceName, current, preferred, changes)
	if len(changes) > 0 {
		if err := saveRouteStateForModem(path, modemID, interfaceName, preferred, changes); err != nil {
			return nil, err
		}
	}
	var applied []defaultRouteChange
	for _, change := range changes {
		if err := ops.deleteDefaultRoute(change.Original); err != nil {
			err = errors.Join(fmt.Errorf("delete existing default route: %w", err), cleanupSavedDefaultRouteState(path, interfaceName, applied))
			return applied, err
		}
		if err := ops.addDefaultRoute(change.Replacement); err != nil {
			restoreErr := restoreOriginalDefaultRouteWithOps(change.Original, ops)
			var cleanupErr error
			if restoreErr == nil {
				cleanupErr = cleanupSavedDefaultRouteState(path, interfaceName, applied)
			} else {
				applied = append(applied, change)
			}
			err = errors.Join(err, restoreErr, cleanupErr)
			return applied, fmt.Errorf("add fallback default route: %w", err)
		}
		applied = append(applied, change)
	}
	return applied, nil
}

func logDefaultRouteTakeover(modemID string, interfaceName string, current []netlink.DefaultRoute, preferred []netlink.DefaultRoute, changes []defaultRouteChange) {
	args := []any{
		"modem", modemID,
		"interface", interfaceName,
		"current", fmt.Sprintf("%+v", current),
		"preferred", fmt.Sprintf("%+v", preferred),
	}
	if len(changes) == 0 {
		slog.Debug("default route takeover skipped", args...)
		return
	}
	args = append(args, "changes", fmt.Sprintf("%+v", changes))
	slog.Debug("default route takeover planned", args...)
}

func cleanupSavedDefaultRouteState(path string, interfaceName string, applied []defaultRouteChange) error {
	if len(applied) > 0 {
		return nil
	}
	if err := deleteRouteState(path, interfaceName); err != nil {
		return fmt.Errorf("delete default route state: %w", err)
	}
	return nil
}

func cleanupDefaultRouteChanges(path string, interfaceName string, changes []defaultRouteChange, ops defaultRouteOps) error {
	if len(changes) == 0 {
		return nil
	}
	if err := restoreDefaultRoutesWithOps(changes, ops); err != nil {
		return err
	}
	return deleteRouteState(path, interfaceName)
}

func restoreDefaultRoutesWithOps(changes []defaultRouteChange, ops defaultRouteOps) error {
	var err error
	for i := len(changes) - 1; i >= 0; i-- {
		change := changes[i]
		if restoreErr := restoreOriginalDefaultRouteWithOps(change.Original, ops); restoreErr != nil {
			err = errors.Join(err, restoreErr)
			continue
		}
		err = errors.Join(err, ops.deleteDefaultRoute(change.Replacement))
	}
	return err
}

func restoreOriginalDefaultRouteWithOps(route netlink.DefaultRoute, ops defaultRouteOps) error {
	err := ops.addDefaultRoute(route)
	if errors.Is(err, netlink.ErrDefaultRouteExists) {
		current, listErr := ops.defaultRoutes()
		if listErr != nil {
			return errors.Join(err, fmt.Errorf("list default routes: %w", listErr))
		}
		if defaultRouteExists(route, current) {
			return nil
		}
	}
	return err
}

func restoreStaleDefaultRouteStatesForModem(modemID string, interfaceNames ...string) error {
	return restoreStaleDefaultRouteStatesWithState(defaultRouteStatePath, routeStateRestoreTarget{
		modemID:        modemID,
		interfaceNames: interfaceNames,
	}, netlinkDefaultRouteOps)
}

func restoreStaleDefaultRouteStatesWithState(path string, target routeStateRestoreTarget, ops defaultRouteOps) error {
	entries, err := loadAllRouteStates(path)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	current, err := ops.defaultRoutes()
	if err != nil {
		return fmt.Errorf("list default routes: %w", err)
	}
	targets := target.interfaceSet()
	modemID := strings.TrimSpace(target.modemID)
	scoped := modemID != "" || len(targets) > 0

	var result error
	for _, interfaceName := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[interfaceName]
		if scoped && !routeStateTargetMatches(modemID, targets, interfaceName, entry) {
			continue
		}
		if !scoped && routeStatePreferredPresent(entry.Preferred, current) {
			continue
		}
		restoreErr := restoreStaleDefaultRouteState(path, interfaceName, entry, current, ops)
		if restoreErr != nil {
			result = errors.Join(result, fmt.Errorf("restore default route state for %s: %w", interfaceName, restoreErr))
		}
	}
	return result
}

func routeStateTargetMatches(modemID string, targets map[string]struct{}, interfaceName string, entry savedRouteState) bool {
	if modemID != "" {
		owner := strings.TrimSpace(entry.ModemID)
		if owner != "" {
			return owner == modemID
		}
	}
	_, ok := targets[interfaceName]
	return ok
}

func (t routeStateRestoreTarget) interfaceSet() map[string]struct{} {
	targets := make(map[string]struct{}, len(t.interfaceNames))
	for _, interfaceName := range t.interfaceNames {
		interfaceName = strings.TrimSpace(interfaceName)
		if interfaceName == "" {
			continue
		}
		targets[interfaceName] = struct{}{}
	}
	return targets
}

func restoreStaleDefaultRouteState(path string, interfaceName string, entry savedRouteState, current []netlink.DefaultRoute, ops defaultRouteOps) error {
	if err := deleteDefaultRoutesWithOps(existingDefaultRoutes(entry.Preferred, current), ops); err != nil {
		return err
	}
	if err := restoreDefaultRoutesWithOps(entry.Changes, ops); err != nil {
		return err
	}
	return deleteRouteState(path, interfaceName)
}

func deleteDefaultRoutesWithOps(routes []netlink.DefaultRoute, ops defaultRouteOps) error {
	var err error
	for i := len(routes) - 1; i >= 0; i-- {
		err = errors.Join(err, ops.deleteDefaultRoute(routes[i]))
	}
	return err
}

func existingDefaultRoutes(routes, current []netlink.DefaultRoute) []netlink.DefaultRoute {
	var result []netlink.DefaultRoute
	for _, route := range routes {
		if defaultRouteExists(route, current) {
			result = append(result, route)
		}
	}
	return result
}

func routeStatePreferredPresent(preferred, current []netlink.DefaultRoute) bool {
	for _, route := range preferred {
		if defaultRouteExists(route, current) {
			return true
		}
	}
	return false
}

func defaultRouteExists(route netlink.DefaultRoute, routes []netlink.DefaultRoute) bool {
	for _, existing := range routes {
		if sameDefaultRoute(route, existing) {
			return true
		}
	}
	return false
}

func defaultRouteChanges(current, preferred []netlink.DefaultRoute) []defaultRouteChange {
	used := slices.Clone(current)
	var changes []defaultRouteChange
	for _, route := range current {
		if defaultRouteExists(route, preferred) {
			continue
		}
		metric, ok := preferredMetric(route.Family, preferred)
		if !ok || route.Metric > metric {
			continue
		}
		replacement := route
		replacement.Metric = fallbackMetric(route, used)
		changes = append(changes, defaultRouteChange{
			Original:    route,
			Replacement: replacement,
		})
		used = append(used, replacement)
	}
	return changes
}

func preferredMetric(family int, routes []netlink.DefaultRoute) (int, bool) {
	metric := 0
	found := false
	for _, route := range routes {
		if route.Family != family {
			continue
		}
		if !found || route.Metric < metric {
			metric = route.Metric
			found = true
		}
	}
	return metric, found
}

func fallbackMetric(route netlink.DefaultRoute, used []netlink.DefaultRoute) int {
	metric := defaultRouteMetric + route.Metric + 1
	for routeMetricExists(route, metric, used) {
		metric++
	}
	return metric
}

func routeMetricExists(route netlink.DefaultRoute, metric int, routes []netlink.DefaultRoute) bool {
	for _, existing := range routes {
		if existing.Family == route.Family && existing.Metric == metric {
			return true
		}
	}
	return false
}

func sameDefaultRoute(a, b netlink.DefaultRoute) bool {
	return a.Interface == b.Interface &&
		a.Family == b.Family &&
		routeProtocol(a) == routeProtocol(b) &&
		a.Scope == b.Scope &&
		a.Gateway == b.Gateway &&
		a.Metric == b.Metric
}

func routeProtocol(route netlink.DefaultRoute) int {
	if route.Protocol != 0 {
		return route.Protocol
	}
	return unix.RTPROT_STATIC
}

func routeMetric(defaultRoute bool) int {
	if defaultRoute {
		return defaultRouteMetric
	}
	return secondaryRouteMetric
}
