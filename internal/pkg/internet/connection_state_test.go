package internet

import (
	"context"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestDBConnectionStateProxy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		modemID       string
		interfaceName string
	}{
		{name: "stores proxy owner by interface", modemID: "modem-1", interfaceName: "wws0"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := testDBConnectionState(t)
			ctx := context.Background()
			if err := state.saveProxyStateForModem(ctx, tt.modemID, tt.interfaceName); err != nil {
				t.Fatalf("saveProxyStateForModem() error = %v", err)
			}

			got, ok, err := state.loadProxyStateForModem(ctx, tt.modemID, tt.interfaceName)
			if err != nil {
				t.Fatalf("loadProxyStateForModem() error = %v", err)
			}
			if !ok || !got {
				t.Fatalf("loadProxyStateForModem() = %v, ok %t; want true, true", got, ok)
			}

			interfaces, err := state.proxyInterfacesForModem(ctx, tt.modemID)
			if err != nil {
				t.Fatalf("proxyInterfacesForModem() error = %v", err)
			}
			if len(interfaces) != 1 || interfaces[0] != tt.interfaceName {
				t.Fatalf("proxyInterfacesForModem() = %#v, want %#v", interfaces, []string{tt.interfaceName})
			}

			if err := state.deleteProxyState(ctx, tt.interfaceName); err != nil {
				t.Fatalf("deleteProxyState() error = %v", err)
			}
			if _, ok, err := state.loadProxyStateForModem(ctx, tt.modemID, tt.interfaceName); err != nil || ok {
				t.Fatalf("loadProxyStateForModem() after delete = ok %t, err %v; want false, nil", ok, err)
			}
		})
	}
}

func TestDBConnectionStateProxyOwnership(t *testing.T) {
	t.Parallel()

	state := testDBConnectionState(t)
	ctx := context.Background()
	if err := state.saveProxyStateForModem(ctx, "modem-1", "wws-old"); err != nil {
		t.Fatalf("saveProxyStateForModem(old) error = %v", err)
	}
	if err := state.saveProxyStateForModem(ctx, "modem-2", "wws-other"); err != nil {
		t.Fatalf("saveProxyStateForModem(other) error = %v", err)
	}
	if err := state.saveProxyStateForModem(ctx, "modem-1", "wws-new"); err != nil {
		t.Fatalf("saveProxyStateForModem(new) error = %v", err)
	}

	if _, ok, err := state.loadProxyStateForModem(ctx, "modem-1", "wws-old"); err != nil || ok {
		t.Fatalf("loadProxyStateForModem(old) = ok %t, err %v; want false, nil", ok, err)
	}
	if enabled, ok, err := state.loadProxyStateForModem(ctx, "modem-1", "wws-new"); err != nil || !ok || !enabled {
		t.Fatalf("loadProxyStateForModem(new) = %t, ok %t, err %v; want true, true, nil", enabled, ok, err)
	}
	if err := state.saveProxyStateForModem(ctx, "modem-1", "wws-other"); err == nil {
		t.Fatal("saveProxyStateForModem(owned interface) error = nil, want error")
	}
}

func TestDBConnectionStateRoute(t *testing.T) {
	t.Parallel()

	original := netlink.DefaultRoute{
		Interface: "eth0",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.0.0.1"),
		Metric:    defaultRouteMetric,
	}
	replacement := original
	replacement.Metric = secondaryRouteMetric
	changes := []defaultRouteChange{{Original: original, Replacement: replacement}}

	tests := []struct {
		name          string
		modemID       string
		interfaceName string
	}{
		{name: "stores route changes by interface", modemID: "modem-1", interfaceName: "wws0"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := testDBConnectionState(t)
			ctx := context.Background()
			if err := state.saveRouteStateForModem(ctx, tt.modemID, tt.interfaceName, []netlink.DefaultRoute{original}, changes); err != nil {
				t.Fatalf("saveRouteStateForModem() error = %v", err)
			}

			got, ok, err := state.loadRouteStateForModem(ctx, tt.modemID, tt.interfaceName)
			if err != nil {
				t.Fatalf("loadRouteStateForModem() error = %v", err)
			}
			if !ok || len(got) != 1 || !sameDefaultRoute(got[0].Original, original) || !sameDefaultRoute(got[0].Replacement, replacement) {
				t.Fatalf("loadRouteStateForModem() = %#v, ok %t; want saved change", got, ok)
			}

			all, err := state.loadAllRouteStates(ctx)
			if err != nil {
				t.Fatalf("loadAllRouteStates() error = %v", err)
			}
			entry, ok := all[tt.interfaceName]
			if !ok || entry.ModemID != tt.modemID || len(entry.Changes) != 1 {
				t.Fatalf("loadAllRouteStates() = %#v, want route state for %s", all, tt.interfaceName)
			}

			if err := state.deleteRouteState(ctx, tt.interfaceName); err != nil {
				t.Fatalf("deleteRouteState() error = %v", err)
			}
			if _, ok, err := state.loadRouteStateForModem(ctx, tt.modemID, tt.interfaceName); err != nil || ok {
				t.Fatalf("loadRouteStateForModem() after delete = ok %t, err %v; want false, nil", ok, err)
			}
		})
	}
}

func TestDBConnectionStateRouteOwnership(t *testing.T) {
	t.Parallel()

	state := testDBConnectionState(t)
	ctx := context.Background()
	changes := []defaultRouteChange{{
		Original:    netlink.DefaultRoute{Interface: "eth0", Family: netlink.FamilyIPv4, Metric: defaultRouteMetric},
		Replacement: netlink.DefaultRoute{Interface: "eth0", Family: netlink.FamilyIPv4, Metric: secondaryRouteMetric},
	}}
	if err := state.saveRouteStateForModem(ctx, "modem-1", "wws0", nil, changes); err != nil {
		t.Fatalf("saveRouteStateForModem() error = %v", err)
	}
	if err := state.saveRouteStateForModem(ctx, "modem-1", "wws0", nil, changes); err == nil {
		t.Fatal("saveRouteStateForModem(overwrite) error = nil, want error")
	}
	if got, ok, err := state.loadRouteStateForModem(ctx, "modem-2", "wws0"); err != nil || ok || got != nil {
		t.Fatalf("loadRouteStateForModem(other modem) = %#v, ok %t, err %v; want nil, false, nil", got, ok, err)
	}

	updated := []defaultRouteChange{{Original: changes[0].Replacement, Replacement: changes[0].Original}}
	if err := state.putRouteStateForModem(ctx, "modem-1", "wws0", nil, updated); err != nil {
		t.Fatalf("putRouteStateForModem() error = %v", err)
	}
	got, ok, err := state.loadRouteStateForModem(ctx, "modem-1", "wws0")
	if err != nil || !ok || len(got) != 1 || !sameDefaultRoute(got[0].Original, updated[0].Original) {
		t.Fatalf("loadRouteStateForModem(updated) = %#v, ok %t, err %v; want updated state", got, ok, err)
	}
}

func TestDBConnectionStateRejectsMissingOwner(t *testing.T) {
	t.Parallel()

	state := testDBConnectionState(t)
	ctx := context.Background()
	if err := state.saveProxyStateForModem(ctx, "", "wws0"); err == nil {
		t.Fatal("saveProxyStateForModem() error = nil, want missing modem error")
	}
	if err := state.saveRouteStateForModem(ctx, "", "wws0", nil, nil); err == nil {
		t.Fatal("saveRouteStateForModem() error = nil, want missing modem error")
	}

	if err := state.store.Put(ctx, interfaceScope("wws0"), proxyKVKey, proxyStateEntry{}); err != nil {
		t.Fatalf("store ownerless proxy state: %v", err)
	}
	if _, ok, err := state.loadProxyStateForModem(ctx, "modem-1", "wws0"); err != nil || ok {
		t.Fatalf("loadProxyStateForModem() = ok %t, err %v; want false, nil", ok, err)
	}

	if err := state.store.Put(ctx, interfaceScope("wws0"), routeKVKey, routeStateEntry{}); err != nil {
		t.Fatalf("store ownerless route state: %v", err)
	}
	if _, ok, err := state.loadRouteStateForModem(ctx, "modem-1", "wws0"); err != nil || ok {
		t.Fatalf("loadRouteStateForModem() = ok %t, err %v; want false, nil", ok, err)
	}
	if _, err := state.loadAllRouteStates(ctx); err == nil {
		t.Fatal("loadAllRouteStates() error = nil, want missing owner error")
	}
}

func testDBConnectionState(t *testing.T) dbConnectionState {
	t.Helper()
	return dbConnectionState{store: testStore(t)}
}

func saveRouteState(state connectionStateStore, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error {
	return state.saveRouteStateForModem(context.Background(), "modem-1", interfaceName, preferred, changes)
}

func loadRouteState(state connectionStateStore, interfaceName string) ([]defaultRouteChange, bool, error) {
	entries, err := state.loadAllRouteStates(context.Background())
	if err != nil {
		return nil, false, err
	}
	entry, ok := entries[interfaceName]
	return entry.Changes, ok, nil
}

func takeoverDefaultRoutesWithState(state connectionStateStore, modemID string, interfaceName string, preferred []netlink.DefaultRoute, ops defaultRouteOps) ([]defaultRouteChange, error) {
	return takeoverDefaultRoutesWithStore(context.Background(), state, modemID, interfaceName, preferred, ops)
}

func cleanupDefaultRouteChanges(state connectionStateStore, interfaceName string, changes []defaultRouteChange, ops defaultRouteOps) error {
	return cleanupDefaultRouteChangesWithStore(context.Background(), state, interfaceName, changes, ops)
}

func restoreStaleDefaultRouteStatesWithState(state connectionStateStore, target routeStateRestoreTarget, ops defaultRouteOps) error {
	return restoreStaleDefaultRouteStatesWithStore(context.Background(), state, target, ops)
}

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}
