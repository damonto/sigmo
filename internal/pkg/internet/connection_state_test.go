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

func testDBConnectionState(t *testing.T) dbConnectionState {
	t.Helper()
	return dbConnectionState{store: testStore(t)}
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
