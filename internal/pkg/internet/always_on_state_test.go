package internet

import (
	"context"
	"errors"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestConnectorAlwaysOnStatePolicy(t *testing.T) {
	t.Parallel()

	const modemID = "modem-1"
	const profileID = "8901000000000000000"

	tests := []struct {
		name      string
		run       func(t *testing.T, c *Connector)
		wantState bool
		wantPrefs Preferences
	}{
		{
			name: "sync enabled writes state",
			run: func(t *testing.T, c *Connector) {
				t.Helper()

				prefs := Preferences{APN: "internet", DefaultRoute: true, ProxyEnabled: true, AlwaysOn: true}
				if err := c.syncAlwaysOnState(context.Background(), profileID, prefs); err != nil {
					t.Fatalf("syncAlwaysOnState() error = %v", err)
				}
				c.preferences[modemID] = prefs
			},
			wantState: true,
			wantPrefs: Preferences{APN: "internet", DefaultRoute: true, ProxyEnabled: true, AlwaysOn: true},
		},
		{
			name: "manual clear deletes state and keeps disabled preferences",
			run: func(t *testing.T, c *Connector) {
				t.Helper()

				prefs := Preferences{APN: "internet", DefaultRoute: true, ProxyEnabled: true, AlwaysOn: true}
				if err := c.syncAlwaysOnState(context.Background(), profileID, prefs); err != nil {
					t.Fatalf("syncAlwaysOnState() error = %v", err)
				}
				modem := fakeInternetModem{modemID: modemID, iccidValue: profileID}
				if err := c.clearAlwaysOnState(context.Background(), modem); err != nil {
					t.Fatalf("clearAlwaysOnState() error = %v", err)
				}
			},
			wantPrefs: Preferences{APN: "internet", DefaultRoute: true, ProxyEnabled: true},
		},
		{
			name: "sync disabled deletes state",
			run: func(t *testing.T, c *Connector) {
				t.Helper()

				prefs := Preferences{APN: "internet", AlwaysOn: true}
				if err := c.syncAlwaysOnState(context.Background(), profileID, prefs); err != nil {
					t.Fatalf("syncAlwaysOnState() setup error = %v", err)
				}
				if err := c.syncAlwaysOnState(context.Background(), profileID, Preferences{APN: "internet"}); err != nil {
					t.Fatalf("syncAlwaysOnState() error = %v", err)
				}
				c.preferences[modemID] = Preferences{APN: "internet"}
			},
			wantPrefs: Preferences{APN: "internet"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, err := NewConnector(ConnectorConfig{State: testStore(t)})
			if err != nil {
				t.Fatalf("NewConnector() error = %v", err)
			}
			tt.run(t, c)

			_, ok, err := c.loadAlwaysOnStateForProfile(context.Background(), profileID)
			if err != nil {
				t.Fatalf("loadAlwaysOnStateForProfile() error = %v", err)
			}
			if ok != tt.wantState {
				t.Fatalf("loadAlwaysOnStateForProfile() ok = %t, want %t", ok, tt.wantState)
			}
			if got := c.preference(modemID); got != tt.wantPrefs {
				t.Fatalf("preference() = %#v, want %#v", got, tt.wantPrefs)
			}
		})
	}
}

func TestAlwaysOnStateUsesCurrentProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modem   fakeInternetModem
		states  map[string]Preferences
		wantAPN string
	}{
		{
			name:  "first profile",
			modem: fakeInternetModem{modemID: "modem-1", iccidValue: "8901000000000000001"},
			states: map[string]Preferences{
				"8901000000000000001": {APN: "first", AlwaysOn: true},
				"8901000000000000002": {APN: "second", AlwaysOn: true},
			},
			wantAPN: "first",
		},
		{
			name:  "second profile",
			modem: fakeInternetModem{modemID: "modem-1", iccidValue: "8901000000000000002"},
			states: map[string]Preferences{
				"8901000000000000001": {APN: "first", AlwaysOn: true},
				"8901000000000000002": {APN: "second", AlwaysOn: true},
			},
			wantAPN: "second",
		},
		{
			name:  "unknown profile falls back to runtime preference",
			modem: fakeInternetModem{modemID: "modem-1", iccidValue: "8901000000000000003"},
			states: map[string]Preferences{
				"8901000000000000001": {APN: "first", AlwaysOn: true},
			},
			wantAPN: "runtime",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, err := NewConnector(ConnectorConfig{State: testStore(t)})
			if err != nil {
				t.Fatalf("NewConnector() error = %v", err)
			}
			c.setPreference(tt.modem.modemID, Preferences{APN: "runtime"})
			for profileID, prefs := range tt.states {
				if err := c.syncAlwaysOnState(context.Background(), profileID, prefs); err != nil {
					t.Fatalf("syncAlwaysOnState(%s) error = %v", profileID, err)
				}
			}

			got, err := c.current(context.Background(), tt.modem)
			if err != nil {
				t.Fatalf("current() error = %v", err)
			}
			if got.APN != tt.wantAPN {
				t.Fatalf("current() APN = %q, want %q", got.APN, tt.wantAPN)
			}
		})
	}
}

func TestAlwaysOnRequiresProfileID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		prefs          Preferences
		wantProfileErr bool
	}{
		{name: "always on requires profile", prefs: Preferences{APN: "internet", AlwaysOn: true}, wantProfileErr: true},
		{name: "manual connection does not require profile", prefs: Preferences{APN: "internet"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, err := NewConnector(ConnectorConfig{State: testStore(t)})
			if err != nil {
				t.Fatalf("NewConnector() error = %v", err)
			}
			_, err = c.connect(context.Background(), fakeInternetModem{modemID: "modem-1"}, tt.prefs, true)
			if errors.Is(err, ErrProfileIDRequired) != tt.wantProfileErr {
				t.Fatalf("connect() error = %v, profile error = %t, want %t", err, errors.Is(err, ErrProfileIDRequired), tt.wantProfileErr)
			}
		})
	}
}

func TestDisconnectClearsAlwaysOnButRestoreCleanupKeepsProfileState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		clearAlwaysOn     bool
		wantAlwaysOnState bool
	}{
		{name: "restore cleanup keeps state", clearAlwaysOn: false, wantAlwaysOnState: true},
		{name: "manual disconnect clears state", clearAlwaysOn: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, err := NewConnector(ConnectorConfig{State: testStore(t)})
			if err != nil {
				t.Fatalf("NewConnector() error = %v", err)
			}
			modem := fakeInternetModem{modemID: "modem-1", iccidValue: "8901000000000000000"}
			prefs := Preferences{APN: "internet", AlwaysOn: true}
			if err := c.syncAlwaysOnState(context.Background(), modem.iccidValue, prefs); err != nil {
				t.Fatalf("syncAlwaysOnState() error = %v", err)
			}

			if err := c.disconnect(context.Background(), modem, tt.clearAlwaysOn); err != nil {
				t.Fatalf("disconnect() error = %v", err)
			}

			_, ok, err := c.loadAlwaysOnStateForProfile(context.Background(), modem.iccidValue)
			if err != nil {
				t.Fatalf("loadAlwaysOnStateForProfile() error = %v", err)
			}
			if ok != tt.wantAlwaysOnState {
				t.Fatalf("loadAlwaysOnStateForProfile() ok = %t, want %t", ok, tt.wantAlwaysOnState)
			}
		})
	}
}

func TestRestoreAlwaysOnSkipsStaleSnapshotAfterManualClear(t *testing.T) {
	t.Parallel()

	const modemID = "modem-1"
	const profileID = "8901000000000000000"

	c, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	prefs := Preferences{APN: "internet", DefaultRoute: true, AlwaysOn: true}
	if err := c.syncAlwaysOnState(context.Background(), profileID, prefs); err != nil {
		t.Fatalf("syncAlwaysOnState() error = %v", err)
	}
	states, err := c.loadAlwaysOnStates(context.Background())
	if err != nil {
		t.Fatalf("loadAlwaysOnStates() error = %v", err)
	}
	stale := states[profileID]
	modem := &mmodem.Modem{EquipmentIdentifier: modemID, Sim: &mmodem.SIM{Identifier: profileID}}
	if err := c.clearAlwaysOnState(context.Background(), modemAccess{modem: modem}); err != nil {
		t.Fatalf("clearAlwaysOnState() error = %v", err)
	}

	if err := c.restoreAlwaysOn(context.Background(), modem, stale); err != nil {
		t.Fatalf("restoreAlwaysOn() error = %v", err)
	}
	_, ok, err := c.loadAlwaysOnStateForProfile(context.Background(), profileID)
	if err != nil {
		t.Fatalf("loadAlwaysOnStateForProfile() error = %v", err)
	}
	if ok {
		t.Fatal("loadAlwaysOnStateForProfile() ok = true, want false")
	}
}
