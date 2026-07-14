package internet

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/netlink"
)

func TestUpdateTrackedDefaultRoute(t *testing.T) {
	previousOps := netlinkDefaultRouteOps
	t.Cleanup(func() { netlinkDefaultRouteOps = previousOps })

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
	netlinkDefaultRouteOps = defaultRouteOps{
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
	tracked := trackedConnection{
		interfaceName: "wwan0",
		prefs:         Preferences{APN: "internet", DefaultRoute: false},
		routes:        []netlink.DefaultRoute{cellular},
		routeMetric:   secondaryRouteMetric,
	}

	updated, err := connector.updateTrackedDefaultRoute(context.Background(), "modem-1", tracked, true)
	if err != nil {
		t.Fatalf("enable default route: %v", err)
	}
	if !updated.prefs.DefaultRoute || updated.routeMetric != defaultRouteMetric {
		t.Fatalf("enabled route state = %+v, want default route metric %d", updated, defaultRouteMetric)
	}

	updated, err = connector.updateTrackedDefaultRoute(context.Background(), "modem-1", updated, false)
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

	updated, err := connector.updateTrackedPreferences(context.Background(), modem, tracked, ConnectionPreferences{AlwaysOn: true})
	if err != nil {
		t.Fatalf("enable Always On: %v", err)
	}
	if !updated.prefs.AlwaysOn {
		t.Fatal("updated preferences AlwaysOn = false, want true")
	}
	if _, ok, err := connector.loadAlwaysOnStateForProfile(context.Background(), "profile-1"); err != nil || !ok {
		t.Fatalf("load Always On state = ok %t, err %v; want saved state", ok, err)
	}

	updated, err = connector.updateTrackedPreferences(context.Background(), modem, updated, ConnectionPreferences{})
	if err != nil {
		t.Fatalf("disable Always On: %v", err)
	}
	if updated.prefs.AlwaysOn {
		t.Fatal("updated preferences AlwaysOn = true, want false")
	}
	if _, ok, err := connector.loadAlwaysOnStateForProfile(context.Background(), "profile-1"); err != nil || ok {
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
		prefs: Preferences{APN: "internet"},
		tracked: []trackedConnection{{
			interfaceName: "qmimux0",
			prefs:         Preferences{APN: "internet"},
		}},
	}

	updated, err := connector.updateQMAPPreferences(context.Background(), modem, connection, ConnectionPreferences{AlwaysOn: true})
	if err != nil {
		t.Fatalf("update QMAP preferences: %v", err)
	}
	if !updated.prefs.AlwaysOn || !updated.tracked[0].prefs.AlwaysOn {
		t.Fatalf("QMAP preferences = %+v, want Always On enabled", updated)
	}
	if got := connector.qmapConnectionFor("modem-1"); got != updated {
		t.Fatalf("stored QMAP connection = %p, want %p", got, updated)
	}
	if _, ok, err := connector.loadAlwaysOnStateForProfile(context.Background(), "profile-1"); err != nil || !ok {
		t.Fatalf("load QMAP Always On state = ok %t, err %v; want saved state", ok, err)
	}
}

func TestUpdateQMAPPreferencesDisablesDefaultRoutesInReverseOrder(t *testing.T) {
	previousOps := netlinkDefaultRouteOps
	t.Cleanup(func() { netlinkDefaultRouteOps = previousOps })

	otherOriginal := netlink.DefaultRoute{Interface: "eth0", Family: netlink.FamilyIPv4, Metric: defaultRouteMetric}
	otherReplacement := otherOriginal
	otherReplacement.Metric = defaultRouteMetric + 11
	firstOriginal := netlink.DefaultRoute{Interface: "qmimux0", Family: netlink.FamilyIPv4, Metric: defaultRouteMetric}
	firstReplacement := firstOriginal
	firstReplacement.Metric = defaultRouteMetric + 12
	secondOriginal := netlink.DefaultRoute{Interface: "qmimux1", Family: netlink.FamilyIPv4, Metric: defaultRouteMetric}
	current := []netlink.DefaultRoute{otherReplacement, firstReplacement, secondOriginal}
	netlinkDefaultRouteOps = defaultRouteOps{
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
	firstChanges := []defaultRouteChange{{Original: otherOriginal, Replacement: otherReplacement}}
	secondChanges := []defaultRouteChange{{Original: firstOriginal, Replacement: firstReplacement}}
	if err := connector.persistence.saveRouteStateForModem(context.Background(), "modem-1", "qmimux0", []netlink.DefaultRoute{firstOriginal}, firstChanges); err != nil {
		t.Fatalf("save first route state: %v", err)
	}
	if err := connector.persistence.saveRouteStateForModem(context.Background(), "modem-1", "qmimux1", []netlink.DefaultRoute{secondOriginal}, secondChanges); err != nil {
		t.Fatalf("save second route state: %v", err)
	}
	connection := &qmapConnection{
		prefs: Preferences{DefaultRoute: true},
		tracked: []trackedConnection{
			{interfaceName: "qmimux0", prefs: Preferences{DefaultRoute: true}, routes: []netlink.DefaultRoute{firstOriginal}, routeChanges: firstChanges},
			{interfaceName: "qmimux1", prefs: Preferences{DefaultRoute: true}, routes: []netlink.DefaultRoute{secondOriginal}, routeChanges: secondChanges},
		},
	}

	updated, err := connector.updateQMAPPreferences(
		context.Background(),
		fakeInternetModem{modemID: "modem-1"},
		connection,
		ConnectionPreferences{},
	)
	if err != nil {
		t.Fatalf("disable QMAP default route: %v", err)
	}
	if updated.prefs.DefaultRoute {
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

func TestQMAPRouteUpdateOrder(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		enabled bool
		want    []int
	}{
		{name: "enable uses connection order", count: 3, enabled: true, want: []int{0, 1, 2}},
		{name: "disable reverses connection order", count: 3, want: []int{2, 1, 0}},
		{name: "empty connection", enabled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qmapRouteUpdateOrder(tt.count, tt.enabled); !slices.Equal(got, tt.want) {
				t.Fatalf("qmapRouteUpdateOrder() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpdateTrackedPreferencesRollsBackRoutesWhenProxyFails(t *testing.T) {
	previousOps := netlinkDefaultRouteOps
	t.Cleanup(func() { netlinkDefaultRouteOps = previousOps })

	route := netlink.DefaultRoute{
		Interface: "wwan0",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.0.0.1"),
		Source:    netip.MustParseAddr("10.0.0.2"),
		Metric:    secondaryRouteMetric,
	}
	current := []netlink.DefaultRoute{route}
	netlinkDefaultRouteOps = defaultRouteOps{
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
	_, err = connector.updateTrackedPreferences(
		context.Background(),
		fakeInternetModem{modemID: "modem-1", iccidValue: "profile-1"},
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
