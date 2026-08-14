package internet

import (
	"errors"
	"net/netip"
	"slices"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/netlink"
)

func TestUpdateTrackedDefaultRoute(t *testing.T) {
	cellular := netlink.DefaultRoute{
		Interface: "wwan0",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.0.0.1"),
		Source:    netip.MustParseAddr("10.0.0.2"),
		Metric:    secondaryRouteMetric,
	}
	other := netlink.DefaultRoute{
		Interface: "eth0",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("192.0.2.1"),
		Source:    netip.MustParseAddr("192.0.2.2"),
		Metric:    defaultRouteMetric,
	}
	current := []netlink.DefaultRoute{cellular, other}
	routeOps := defaultRouteOps{
		defaultRoutes: func() ([]netlink.DefaultRoute, error) { return slices.Clone(current), nil },
		addDefaultRoute: func(route netlink.DefaultRoute) error {
			if defaultRouteExists(route, current) {
				return netlink.ErrDefaultRouteExists
			}
			current = append(current, route)
			return nil
		},
		deleteDefaultRoute: func(route netlink.DefaultRoute) error {
			for i, candidate := range current {
				if sameDefaultRoute(candidate, route) {
					current = append(current[:i], current[i+1:]...)
					break
				}
			}
			return nil
		},
	}

	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	connector.routes = routeOps
	tracked := trackedConnection{
		interfaceName: "wwan0",
		prefs:         Preferences{APN: "internet", DefaultRoute: false},
		routes:        []netlink.DefaultRoute{cellular},
		routeMetric:   secondaryRouteMetric,
	}

	updated, err := connector.updateTrackedDefaultRoute(t.Context(), "modem-1", tracked, true)
	if err != nil {
		t.Fatalf("enable default route: %v", err)
	}
	if !updated.prefs.DefaultRoute || updated.routeMetric != defaultRouteMetric {
		t.Fatalf("enabled route state = %+v, want default route metric %d", updated, defaultRouteMetric)
	}

	updated, err = connector.updateTrackedDefaultRoute(t.Context(), "modem-1", updated, false)
	if err != nil {
		t.Fatalf("disable default route: %v", err)
	}
	if updated.prefs.DefaultRoute || updated.routeMetric < secondaryRouteMetric {
		t.Fatalf("disabled route state = %+v, want secondary route", updated)
	}
}

func TestUpdateTrackedPreferencesPersistsAlwaysOn(t *testing.T) {
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	modem := fakeInternetModem{modemID: "modem-1", iccidValue: "profile-1"}
	tracked := trackedConnection{
		interfaceName: "wwan0",
		prefs:         Preferences{APN: "internet"},
	}

	updated, err := connector.updateTrackedPreferences(t.Context(), modem, tracked, ConnectionPreferences{AlwaysOn: true})
	if err != nil {
		t.Fatalf("enable Always On: %v", err)
	}
	if !updated.prefs.AlwaysOn {
		t.Fatal("updated preferences AlwaysOn = false, want true")
	}
	if _, ok, err := connector.loadAlwaysOnStateForProfile(t.Context(), "profile-1"); err != nil || !ok {
		t.Fatalf("load Always On state = ok %t, err %v; want saved state", ok, err)
	}

	updated, err = connector.updateTrackedPreferences(t.Context(), modem, updated, ConnectionPreferences{})
	if err != nil {
		t.Fatalf("disable Always On: %v", err)
	}
	if updated.prefs.AlwaysOn {
		t.Fatal("updated preferences AlwaysOn = true, want false")
	}
	if _, ok, err := connector.loadAlwaysOnStateForProfile(t.Context(), "profile-1"); err != nil || ok {
		t.Fatalf("load cleared Always On state = ok %t, err %v; want absent", ok, err)
	}
}

func TestUpdateQMAPPreferencesPersistsAlwaysOn(t *testing.T) {
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	modem := fakeInternetModem{modemID: "modem-1", iccidValue: "profile-1"}
	connection := &qmapConnection{
		tracked: trackedConnection{
			interfaceName: "qmimux0",
			prefs:         Preferences{APN: "internet"},
		},
	}

	updated, err := connector.updateQMAPPreferences(t.Context(), modem, connection, ConnectionPreferences{AlwaysOn: true})
	if err != nil {
		t.Fatalf("update QMAP preferences: %v", err)
	}
	if !updated.tracked.prefs.AlwaysOn {
		t.Fatalf("QMAP preferences = %+v, want Always On enabled", updated)
	}
	if got := connector.qmapConnectionFor("modem-1", 0); got != updated {
		t.Fatalf("stored QMAP connection = %p, want %p", got, updated)
	}
	if _, ok, err := connector.loadAlwaysOnStateForProfile(t.Context(), "profile-1"); err != nil || !ok {
		t.Fatalf("load QMAP Always On state = ok %t, err %v; want saved state", ok, err)
	}
}

func TestUpdateQMAPPreferencesDisablesDefaultRoute(t *testing.T) {
	otherOriginal := netlink.DefaultRoute{Interface: "eth0", Family: netlink.FamilyIPv4, Metric: defaultRouteMetric}
	otherReplacement := otherOriginal
	otherReplacement.Metric = defaultRouteMetric + 11
	qmapRoute := netlink.DefaultRoute{Interface: "qmimux0", Family: netlink.FamilyIPv4, Metric: defaultRouteMetric}
	current := []netlink.DefaultRoute{otherReplacement, qmapRoute}
	routeOps := defaultRouteOps{
		defaultRoutes: func() ([]netlink.DefaultRoute, error) { return slices.Clone(current), nil },
		addDefaultRoute: func(route netlink.DefaultRoute) error {
			if defaultRouteExists(route, current) {
				return netlink.ErrDefaultRouteExists
			}
			current = append(current, route)
			return nil
		},
		deleteDefaultRoute: func(route netlink.DefaultRoute) error {
			for i, candidate := range current {
				if sameDefaultRoute(candidate, route) {
					current = append(current[:i], current[i+1:]...)
					break
				}
			}
			return nil
		},
	}

	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	connector.routes = routeOps
	changes := []defaultRouteChange{{Original: otherOriginal, Replacement: otherReplacement}}
	if err := connector.persistence.saveRouteStateForModem(t.Context(), "modem-1", "qmimux0", []netlink.DefaultRoute{qmapRoute}, changes); err != nil {
		t.Fatalf("save route state: %v", err)
	}
	connection := &qmapConnection{
		tracked: trackedConnection{interfaceName: "qmimux0", prefs: Preferences{DefaultRoute: true}, routes: []netlink.DefaultRoute{qmapRoute}, routeChanges: changes},
	}

	updated, err := connector.updateQMAPPreferences(t.Context(), fakeInternetModem{modemID: "modem-1"},
		connection,
		ConnectionPreferences{},
	)
	if err != nil {
		t.Fatalf("disable QMAP default route: %v", err)
	}
	if updated.tracked.prefs.DefaultRoute {
		t.Fatal("QMAP DefaultRoute = true, want false")
	}
	for _, route := range current {
		if route.Interface != "eth0" && route.Metric == defaultRouteMetric {
			t.Fatalf("cellular route kept default metric: %+v", route)
		}
	}
	if !defaultRouteExists(otherOriginal, current) {
		t.Fatalf("original non-cellular route was not restored: %+v", current)
	}
}

func TestUpdateTrackedPreferencesRollsBackRoutesWhenProxyFails(t *testing.T) {
	route := netlink.DefaultRoute{
		Interface: "wwan0",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.0.0.1"),
		Source:    netip.MustParseAddr("10.0.0.2"),
		Metric:    secondaryRouteMetric,
	}
	current := []netlink.DefaultRoute{route}
	routeOps := defaultRouteOps{
		defaultRoutes: func() ([]netlink.DefaultRoute, error) { return slices.Clone(current), nil },
		addDefaultRoute: func(route netlink.DefaultRoute) error {
			if defaultRouteExists(route, current) {
				return netlink.ErrDefaultRouteExists
			}
			current = append(current, route)
			return nil
		},
		deleteDefaultRoute: func(route netlink.DefaultRoute) error {
			for i, candidate := range current {
				if sameDefaultRoute(candidate, route) {
					current = append(current[:i], current[i+1:]...)
					break
				}
			}
			return nil
		},
	}

	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	connector.routes = routeOps
	_, err = connector.updateTrackedPreferences(t.Context(), fakeInternetModem{modemID: "modem-1", iccidValue: "profile-1"},
		trackedConnection{
			interfaceName: "wwan0",
			prefs:         Preferences{APN: "internet"},
			routes:        []netlink.DefaultRoute{route},
		},
		ConnectionPreferences{DefaultRoute: true, ProxyEnabled: true},
	)
	if !errors.Is(err, ErrProxyNotConfigured) {
		t.Fatalf("update preferences error = %v, want %v", err, ErrProxyNotConfigured)
	}
	if len(current) != 1 || !sameDefaultRoute(current[0], route) {
		t.Fatalf("routes after rollback = %+v, want original route %+v", current, route)
	}
}
