package internet

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	modemlink "github.com/damonto/sigmo/internal/pkg/modem/link"
	"github.com/damonto/sigmo/internal/pkg/networkprefs"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestAirplaneModeRestoresOnlyAlwaysOnQMAPInternet(t *testing.T) {
	tests := []struct {
		name          string
		alwaysOn      bool
		wantReconnect bool
	}{
		{name: "restores Always-On connection", alwaysOn: true, wantReconnect: true},
		{name: "keeps manual connection disconnected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousOpen := openInternetQMAPSession
			previousConfigure := configureInternetQMAPNetwork
			previousRemove := removeInternetQMAPMuxes
			t.Cleanup(func() {
				openInternetQMAPSession = previousOpen
				configureInternetQMAPNetwork = previousConfigure
				removeInternetQMAPMuxes = previousRemove
			})

			openCalls := 0
			openInternetQMAPSession = func(_ context.Context, _ *mmodem.Modem, _ modemlink.QMAPConfig) (*modemlink.QMAPSession, error) {
				openCalls++
				return &modemlink.QMAPSession{InterfaceName: "qmimux0"}, nil
			}
			configureInternetQMAPNetwork = func(
				_ context.Context,
				_ connectionStateStore,
				_ string,
				prefs Preferences,
				_ *modemlink.QMAPSession,
			) (trackedConnection, []string, error) {
				return trackedConnection{
					interfaceName: "qmimux0",
					addresses:     []netip.Prefix{netip.MustParsePrefix("10.0.0.2/30")},
					prefs:         prefs,
				}, []string{"1.1.1.1"}, nil
			}
			removeInternetQMAPMuxes = func(*mmodem.Modem, ...uint8) error { return nil }

			connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
			if err != nil {
				t.Fatalf("NewConnector() error = %v", err)
			}
			const (
				modemID   = "modem-1"
				profileID = "8901000000000000000"
			)
			modem := &mmodem.Modem{
				EquipmentIdentifier: modemID,
				PrimaryPort:         "cdc-wdm0",
				Ports: []mmodem.ModemPort{{
					Device:   "cdc-wdm0",
					PortType: wwanmodem.PortQMI,
				}},
				SIM: &mmodem.SIM{Identifier: profileID},
			}
			prefs := Preferences{APN: "internet", IPType: "ipv4", AlwaysOn: tt.alwaysOn}
			connector.qmapEnabled[modemID] = true
			connector.qmapConnections[modemID] = &qmapConnection{
				modem: modem,
				prefs: prefs,
			}
			connector.preferences[modemID] = prefs
			if tt.alwaysOn {
				if err := connector.syncAlwaysOnState(t.Context(), profileID, prefs); err != nil {
					t.Fatalf("syncAlwaysOnState() error = %v", err)
				}
			}

			err = connector.ChangeAirplaneMode(t.Context(), modem, true, func() (bool, error) {
				if connector.qmapConnection(modem) != nil {
					t.Fatal("QMAP Internet remained connected before radio shutdown")
				}
				if enabled, ok := connector.airplaneModeState(modemID); !ok || !enabled {
					t.Fatalf("airplane mode transition state = %t, found %t; want true, true", enabled, ok)
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("ChangeAirplaneMode(enable) error = %v", err)
			}
			current, err := connector.Current(t.Context(), modem)
			if err != nil {
				t.Fatalf("Current() in airplane mode error = %v", err)
			}
			if current.Status != StatusDisconnected || current.AlwaysOn != tt.alwaysOn {
				t.Fatalf("Current() in airplane mode = %+v, want disconnected with AlwaysOn %t", current, tt.alwaysOn)
			}
			if _, err := connector.Connect(t.Context(), modem, prefs); !errors.Is(err, ErrAirplaneMode) {
				t.Fatalf("Connect() in airplane mode error = %v, want %v", err, ErrAirplaneMode)
			}

			err = connector.ChangeAirplaneMode(t.Context(), modem, false, func() (bool, error) {
				if connector.qmapConnection(modem) != nil {
					t.Fatal("QMAP Internet restored before radio startup")
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("ChangeAirplaneMode(disable) error = %v", err)
			}
			select {
			case restoreModem := <-connector.alwaysOnRestore:
				if restoreModem != modem {
					t.Fatalf("queued modem = %p, want %p", restoreModem, modem)
				}
				if err := connector.restoreAlwaysOn(t.Context(), restoreModem, Preferences{}); err != nil {
					t.Fatalf("restoreAlwaysOn() error = %v", err)
				}
			default:
				t.Fatal("Airplane disable did not queue Always-On restore")
			}
			connected := connector.qmapConnection(modem) != nil
			if connected != tt.wantReconnect {
				t.Fatalf("QMAP Internet restored = %t, want %t", connected, tt.wantReconnect)
			}
			if got := openCalls > 0; got != tt.wantReconnect {
				t.Fatalf("QMAP reconnect attempted = %t, want %t", got, tt.wantReconnect)
			}
			if enabled, ok := connector.airplaneModeState(modemID); !ok || enabled {
				t.Fatalf("airplane mode final state = %t, found %t; want false, true", enabled, ok)
			}
		})
	}
}

func TestAirplaneModeRejectsInternetChangesAndAllowsPolicyClear(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	networkPreferences, err := networkprefs.New(store)
	if err != nil {
		t.Fatalf("networkprefs.New() error = %v", err)
	}
	connector, err := NewConnector(ConnectorConfig{
		State:              store,
		NetworkPreferences: networkPreferences,
	})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	const (
		modemID   = "modem-1"
		profileID = "8901000000000000000"
	)
	modem := &mmodem.Modem{
		EquipmentIdentifier: modemID,
		SIM:                 &mmodem.SIM{Identifier: profileID},
	}
	prefs := Preferences{APN: "internet", AlwaysOn: true}
	connector.preferences[modemID] = prefs
	if err := connector.syncAlwaysOnState(ctx, profileID, prefs); err != nil {
		t.Fatalf("syncAlwaysOnState() error = %v", err)
	}
	if err := networkPreferences.SaveAirplaneMode(ctx, modemID, true); err != nil {
		t.Fatalf("SaveAirplaneMode() error = %v", err)
	}

	if _, err := connector.Connect(ctx, modem, prefs); !errors.Is(err, ErrAirplaneMode) {
		t.Fatalf("Connect() error = %v, want %v", err, ErrAirplaneMode)
	}
	if _, err := connector.UpdatePreferences(ctx, modem, ConnectionPreferences{AlwaysOn: false}); !errors.Is(err, ErrAirplaneMode) {
		t.Fatalf("UpdatePreferences() error = %v, want %v", err, ErrAirplaneMode)
	}
	if err := connector.Disconnect(ctx, modem); err != nil {
		t.Fatalf("Disconnect() in airplane mode error = %v", err)
	}
	if _, ok, err := connector.loadAlwaysOnStateForProfile(ctx, profileID); err != nil {
		t.Fatalf("loadAlwaysOnStateForProfile() error = %v", err)
	} else if ok {
		t.Fatal("Disconnect() in airplane mode kept Always-On policy")
	}
}

func TestAirplaneModeRestoresInternetWithReloadedQMIModem(t *testing.T) {
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	const modemID = "modem-1"
	old := &mmodem.Modem{
		EquipmentIdentifier: modemID,
		PrimaryPort:         "cdc-wdm0",
		Ports: []mmodem.ModemPort{{
			Device:   "cdc-wdm0",
			PortType: wwanmodem.PortQMI,
		}},
	}
	replacement := &mmodem.Modem{
		EquipmentIdentifier: modemID,
		PrimaryPort:         "cdc-wdm0",
		Ports: []mmodem.ModemPort{{
			Device:   "cdc-wdm0",
			PortType: wwanmodem.PortQMI,
		}},
	}
	connector.setAirplaneModeState(modemID, true)
	changed := false
	connector.reloadModem = func(_ context.Context, got *mmodem.Modem) (*mmodem.Modem, error) {
		if got != old {
			t.Fatalf("Reload modem = %p, want old modem %p", got, old)
		}
		if !changed {
			t.Fatal("QMI modem reloaded before radio change")
		}
		return replacement, nil
	}

	err = connector.ChangeAirplaneMode(t.Context(), old, false, func() (bool, error) {
		changed = true
		return true, nil
	})
	if err != nil {
		t.Fatalf("ChangeAirplaneMode() error = %v", err)
	}
	select {
	case got := <-connector.alwaysOnRestore:
		if got != replacement {
			t.Fatalf("queued modem = %p, want replacement %p", got, replacement)
		}
	default:
		t.Fatal("Airplane disable did not queue Internet restore")
	}
	if enabled, ok := connector.airplaneModeState(modemID); !ok || enabled {
		t.Fatalf("airplane mode state = %t, found %t; want false, true", enabled, ok)
	}
}
