//go:build ims

package ims

import (
	"errors"
	"net/netip"
	"path/filepath"
	"slices"
	"testing"

	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestResolveWiFiCallingSettings(t *testing.T) {
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	tests := []struct {
		name     string
		settings WiFiCallingSettings
		want     UnderlaySettings
		wantErr  bool
	}{
		{name: "defaults to system", want: UnderlaySettings{Mode: UnderlayModeSystem}},
		{name: "normalizes self", settings: WiFiCallingSettings{Underlay: UnderlaySettings{Mode: " SELF ", ModemID: "ignored"}}, want: UnderlaySettings{Mode: UnderlayModeSelf}},
		{name: "keeps another modem", settings: WiFiCallingSettings{Underlay: UnderlaySettings{Mode: UnderlayModeModem, ModemID: " modem-2 "}}, want: UnderlaySettings{Mode: UnderlayModeModem, ModemID: "modem-2"}},
		{name: "same modem becomes self", settings: WiFiCallingSettings{Underlay: UnderlaySettings{Mode: UnderlayModeModem, ModemID: "modem-1"}}, want: UnderlaySettings{Mode: UnderlayModeSelf}},
		{name: "modem requires id", settings: WiFiCallingSettings{Underlay: UnderlaySettings{Mode: UnderlayModeModem}}, wantErr: true},
		{name: "rejects unknown mode", settings: WiFiCallingSettings{Underlay: UnderlaySettings{Mode: "auto"}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveWiFiCallingSettings(modem, tt.settings)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveWiFiCallingSettings() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got.Underlay != tt.want {
				t.Fatalf("Underlay = %+v, want %+v", got.Underlay, tt.want)
			}
		})
	}
}

func TestWiFiCallingSettingsStoreUnderlay(t *testing.T) {
	ctx := t.Context()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	settings := newWiFiCallingSettingsStore(store)
	want := WiFiCallingSettings{Enabled: true, Underlay: UnderlaySettings{Mode: UnderlayModeModem, ModemID: "modem-2"}}
	if err := settings.Put(ctx, "profile-1", want); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	var stored wifiCallingSettingsRecord
	if err := store.Get(ctx, "profile:profile-1", keyWiFiCallingSettings, &stored); err != nil {
		t.Fatalf("read stored record: %v", err)
	}
	wantRecord := wifiCallingSettingsRecord{Enabled: want.Enabled, Underlay: want.Underlay}
	if stored != wantRecord {
		t.Fatalf("stored record = %+v, want %+v", stored, wantRecord)
	}
	got, err := settings.Get(ctx, "profile-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != want {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
}

func TestNewCoordinatorDoesNotStoreTypedNilInternet(t *testing.T) {
	got := newCoordinator(coordinatorConfig{})
	if got.internet != nil {
		t.Fatalf("internet = %T, want nil", got.internet)
	}
}

func TestNewCoordinatorDoesNotStoreTypedNilRegistry(t *testing.T) {
	got := newCoordinator(coordinatorConfig{})
	if got.registry != nil {
		t.Fatalf("registry = %T, want nil", got.registry)
	}
}

func TestNewCoordinatorStoresRegistryDependency(t *testing.T) {
	registry := new(mmodem.Registry)
	got := newCoordinator(coordinatorConfig{Registry: registry})
	if got.registry != registry {
		t.Fatalf("registry = %p, want %p", got.registry, registry)
	}
}

func TestFilterAddressesForConnection(t *testing.T) {
	ipv4 := netip.MustParseAddr("192.0.2.1")
	ipv6 := netip.MustParseAddr("2001:db8::1")
	tests := []struct {
		name       string
		connection *pinternet.Connection
		want       []netip.Addr
	}{
		{name: "missing connection", want: []netip.Addr{ipv4, ipv6}},
		{name: "unknown families", connection: &pinternet.Connection{}, want: []netip.Addr{ipv4, ipv6}},
		{name: "IPv4 only", connection: &pinternet.Connection{IPv4Addresses: []string{"10.0.0.2/30"}}, want: []netip.Addr{ipv4}},
		{name: "IPv6 only", connection: &pinternet.Connection{IPv6Addresses: []string{"2001:db8::2/64"}}, want: []netip.Addr{ipv6}},
		{name: "dual stack", connection: &pinternet.Connection{IPv4Addresses: []string{"10.0.0.2/30"}, IPv6Addresses: []string{"2001:db8::2/64"}}, want: []netip.Addr{ipv4, ipv6}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterAddressesForConnection(tt.connection, []netip.Addr{ipv4, ipv6})
			if !slices.Equal(got, tt.want) {
				t.Fatalf("filterAddressesForConnection() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewModemUnderlayAllowsDynamicIPFamilies(t *testing.T) {
	connection := &pinternet.Connection{
		Status:        pinternet.StatusConnected,
		InterfaceName: "wwan0",
		IPv4Addresses: []string{"10.0.0.2/30"},
	}
	underlay, err := newModemUnderlay(t.Context(), &fakeInternetRestorer{connection: connection},
		&mmodem.Modem{EquipmentIdentifier: "modem-1"},
	)
	if err != nil {
		t.Fatalf("newModemUnderlay() error = %v", err)
	}
	if got := underlay.LocalIP(); got.IsValid() {
		t.Fatalf("LocalIP() = %v, want zero value", got)
	}
}

func TestModemUnderlayRequiresConnectedInternet(t *testing.T) {
	tests := []struct {
		name       string
		connection *pinternet.Connection
		currentErr error
		wantErr    bool
	}{
		{name: "missing connection", wantErr: true},
		{name: "disconnected", connection: &pinternet.Connection{Status: pinternet.StatusDisconnected, InterfaceName: "wwan0"}, wantErr: true},
		{name: "missing interface", connection: &pinternet.Connection{Status: pinternet.StatusConnected}, wantErr: true},
		{name: "current error", currentErr: errors.New("transport unavailable"), wantErr: true},
		{name: "connected", connection: &pinternet.Connection{Status: pinternet.StatusConnected, InterfaceName: "wwan0", DNS: []string{"1.1.1.1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			internet := &fakeInternetRestorer{connection: tt.connection, currentErr: tt.currentErr}
			underlay := &modemUnderlay{internet: internet, modem: &mmodem.Modem{EquipmentIdentifier: "modem-1"}}
			_, _, err := underlay.current(t.Context())
			if (err != nil) != tt.wantErr {
				t.Fatalf("current() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrWiFiCallingUnderlayUnavailable) {
				t.Fatalf("current() error = %v, want %v", err, ErrWiFiCallingUnderlayUnavailable)
			}
		})
	}
}
