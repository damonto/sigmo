package ims

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	imsgo "github.com/damonto/ims-go"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestIMSOnlyPersistsFreshAgreedNumbers(t *testing.T) {
	ctx := t.Context()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "ims.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	modem := &mmodem.Modem{EquipmentIdentifier: "modem", SIM: &mmodem.SIM{Identifier: "profile-a"}, Number: "+12025550100"}
	target := modem.SIMIdentity()
	session := &sessionState{id: 1, modem: modem, profileID: target.ICCID, numberTarget: target}
	connectivity := &Connectivity{numberStore: store}
	report := imsgo.RegistrationInfo{Registered: true, MSISDN: "+12025550123"}
	assertNumber := func(want string) {
		t.Helper()
		numbers, err := store.SIMProfileNumbers(ctx, []string{target.ICCID})
		if err != nil || numbers[target.ICCID] != want || modem.Snapshot().Number != want {
			t.Fatalf("number in database = %q, snapshot = %q, want %q, error = %v", numbers[target.ICCID], modem.Snapshot().Number, want, err)
		}
	}
	connectivity.updateNumber(AccessVoLTE, session, report)
	assertNumber(report.MSISDN)

	const manual = "+12025550199"
	if err := modem.UpdateStoredNumber(target, func(mmodem.ModemSnapshot) (string, error) {
		return manual, store.SaveSIMProfileNumber(ctx, target.ICCID, modem.EquipmentIdentifier, manual)
	}); err != nil {
		t.Fatal(err)
	}
	connectivity.updateNumber(AccessVoLTE, session, report)
	connectivity.updateNumber(AccessWiFiCalling, session, imsgo.RegistrationInfo{Registered: true, MSISDNAmbiguous: true})
	connectivity.updateNumber(AccessWiFiCalling, session, imsgo.RegistrationInfo{})
	connectivity.updateNumber(AccessVoLTE, session, imsgo.RegistrationInfo{})
	assertNumber(manual)

	connectivity.updateNumber(AccessVoLTE, session, report)
	assertNumber(report.MSISDN)
	modem.InvalidateSIMActivation()
	connectivity.updateNumber(AccessVoLTE, session, imsgo.RegistrationInfo{Registered: true, MSISDN: "+12025550188"})
	numbers, err := store.SIMProfileNumbers(ctx, []string{target.ICCID})
	if err != nil || numbers[target.ICCID] != report.MSISDN {
		t.Fatalf("stale registration updated the number: %v, %v", numbers, err)
	}
}

func TestIMSNumberResolution(t *testing.T) {
	type step struct {
		access     Access
		registered bool
		number     string
		want       string
		conflict   bool
		ambiguous  bool
	}
	tests := []struct {
		name  string
		own   string
		steps []step
	}{
		{name: "refresh and temporary outage", steps: []step{
			{access: AccessVoLTE, registered: true, number: "+15551234567", want: "+15551234567"},
			{access: AccessVoLTE, want: "+15551234567"},
			{access: AccessVoLTE, registered: true, number: "+15557654321", want: "+15557654321"},
			{access: AccessVoLTE, registered: true},
			{access: AccessVoLTE},
		}},
		{name: "matching access identities", steps: []step{
			{access: AccessWiFiCalling, registered: true, number: "+15551234567", want: "+15551234567"},
			{access: AccessVoLTE, registered: true, number: "+15551234567", want: "+15551234567"},
			{access: AccessWiFiCalling, want: "+15551234567"},
			{access: AccessVoLTE, want: "+15551234567"},
		}},
		{name: "conflict resolves when one access retires", steps: []step{
			{access: AccessWiFiCalling, registered: true, number: "+15551234567", want: "+15551234567"},
			{access: AccessVoLTE, registered: true, number: "+15557654321", conflict: true},
			{access: AccessWiFiCalling, want: "+15557654321"},
		}},
		{name: "empty registration retires cached number", steps: []step{
			{access: AccessWiFiCalling, registered: true, number: "+15551234567", want: "+15551234567"},
			{access: AccessWiFiCalling, want: "+15551234567"},
			{access: AccessVoLTE, registered: true},
			{access: AccessVoLTE},
		}},
		{name: "one ambiguous registration blocks automatic selection", steps: []step{
			{access: AccessWiFiCalling, registered: true, number: "+15551234567", want: "+15551234567"},
			{access: AccessVoLTE, registered: true, ambiguous: true, conflict: true},
			{access: AccessWiFiCalling, conflict: true},
			{access: AccessVoLTE},
		}},
		{name: "modem precedence", own: "+15550000000", steps: []step{
			{access: AccessVoLTE, registered: true, number: "+15551234567", want: "+15550000000"},
			{access: AccessWiFiCalling, registered: true, number: "+15557654321", want: "+15550000000", conflict: true},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modem := &mmodem.Modem{EquipmentIdentifier: "modem-1", SIM: &mmodem.SIM{Identifier: "profile-a"}, Number: tt.own}
			session := &sessionState{id: 1, modem: modem, profileID: "profile-a", numberTarget: modem.Snapshot().SIMIdentity}
			c := &Connectivity{}
			for i, step := range tt.steps {
				c.updateNumber(step.access, session, imsgo.RegistrationInfo{Registered: step.registered, MSISDN: step.number, MSISDNAmbiguous: step.ambiguous})
				if got := modem.Snapshot().Number; got != step.want {
					t.Fatalf("step %d: Number = %q, want %q", i, got, step.want)
				}
				numbers := c.numbers[modem.EquipmentIdentifier]
				if numbers.conflict != step.conflict {
					t.Fatalf("step %d: conflict=%v, want %v", i, numbers.conflict, step.conflict)
				}
			}
		})
	}
}

func TestIMSNumberRejectsStaleUpdates(t *testing.T) {
	tests := []struct {
		name  string
		stale func(*sessionState) *sessionState
	}{
		{name: "old session", stale: func(current *sessionState) *sessionState { old := *current; old.id--; return &old }},
		{name: "different profile", stale: func(current *sessionState) *sessionState { old := *current; old.profileID = "profile-b"; return &old }},
		{name: "missing ICCID", stale: func(current *sessionState) *sessionState {
			old := *current
			old.numberTarget = mmodem.SIMIdentity{}
			return &old
		}},
		{name: "old modem generation", stale: func(current *sessionState) *sessionState {
			old := *current
			old.generation--
			old.modem = &mmodem.Modem{EquipmentIdentifier: "modem-1", SIM: &mmodem.SIM{Identifier: "profile-a"}}
			old.numberTarget = old.modem.Snapshot().SIMIdentity
			return &old
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modem := &mmodem.Modem{EquipmentIdentifier: "modem-1", SIM: &mmodem.SIM{Identifier: "profile-a"}}
			current := &sessionState{id: 2, generation: 2, modem: modem, profileID: "profile-a", numberTarget: modem.Snapshot().SIMIdentity}
			c := &Connectivity{}
			c.updateNumber(AccessVoLTE, current, imsgo.RegistrationInfo{Registered: true, MSISDN: "+15551234567"})
			c.updateNumber(AccessVoLTE, tt.stale(current), imsgo.RegistrationInfo{Registered: true, MSISDN: "+15557654321"})
			if got := modem.Snapshot().Number; got != "+15551234567" {
				t.Fatalf("Number = %q after stale update", got)
			}
			if c.numbers["modem-1"].target != current.numberTarget {
				t.Fatal("stale update replaced the current identity")
			}
		})
	}
}

func TestIMSNumberSessionCleanup(t *testing.T) {
	tests := []struct {
		name    string
		cleanup func(*coordinator, *imsgo.Client)
	}{
		{name: "reconnecting", cleanup: func(c *coordinator, client *imsgo.Client) { c.markClientReconnecting("modem-1", 1, client) }},
		{name: "disconnected", cleanup: func(c *coordinator, client *imsgo.Client) { c.markDisconnected("modem-1", 1, client) }},
		{name: "connecting", cleanup: func(c *coordinator, _ *imsgo.Client) { c.markConnecting("modem-1", 1) }},
		{name: "waiting for uplink", cleanup: func(c *coordinator, _ *imsgo.Client) { c.markWaitingForUplink("modem-1", 1) }},
		{name: "requested reconnect", cleanup: func(c *coordinator, client *imsgo.Client) { c.requestReconnect("modem-1", client) }},
		{name: "detached", cleanup: func(c *coordinator, _ *imsgo.Client) { c.detachSession("modem-1") }},
		{name: "detached by ID", cleanup: func(c *coordinator, _ *imsgo.Client) { c.detachSessionByID("modem-1", 1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modem := &mmodem.Modem{EquipmentIdentifier: "modem-1", SIM: &mmodem.SIM{Identifier: "profile-a"}}
			client := &imsgo.Client{}
			session := &sessionState{id: 1, client: client, modem: modem, profileID: "profile-a", numberTarget: modem.Snapshot().SIMIdentity}
			connectivity := &Connectivity{}
			c := &coordinator{sessions: map[string]*sessionState{"modem-1": session}, closing: true}
			c.onRegistration = func(session *sessionState, info imsgo.RegistrationInfo) {
				connectivity.updateNumber(AccessVoLTE, session, info)
			}
			connectivity.updateNumber(AccessVoLTE, session, imsgo.RegistrationInfo{Registered: true, MSISDN: "+15551234567"})
			tt.cleanup(c, client)
			if modem.Snapshot().Number != "+15551234567" {
				t.Fatal("cleanup did not retain the last observed number")
			}
			connectivity.updateNumber(AccessWiFiCalling, session, imsgo.RegistrationInfo{Registered: true, MSISDN: "+15557654321"})
			if got := modem.Snapshot().Number; got != "+15557654321" {
				t.Fatalf("number after Wi-Fi registration = %q, want +15557654321", got)
			}
		})
	}
}

func TestIMSNumberSyncUsesCurrentSession(t *testing.T) {
	connectivity := NewConnectivity(ConnectivityConfig{})
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1", SIM: &mmodem.SIM{Identifier: "profile-a"}}
	// An idle client has no hardware resources. Its snapshot moves the last
	// observed number into the cache without changing connection timestamps.
	c := connectivity.volte
	client := &imsgo.Client{}
	session := &sessionState{id: 1, client: client, modem: modem, profileID: "profile-a", numberTarget: modem.Snapshot().SIMIdentity, connectedAt: time.Unix(123, 0)}
	c.sessions["modem-1"] = session
	connectivity.updateNumber(AccessVoLTE, session, imsgo.RegistrationInfo{Registered: true, MSISDN: "+15551234567"})
	c.syncRegistration("modem-1", 1, client)
	connectivity.updateNumber(AccessWiFiCalling, session, imsgo.RegistrationInfo{Registered: true, MSISDN: "+15557654321"})
	if got := modem.Snapshot().Number; got != "+15557654321" {
		t.Fatalf("number after sync and Wi-Fi registration = %q, want +15557654321", got)
	}
	if !session.connectedAt.Equal(time.Unix(123, 0)) {
		t.Fatal("registration sync changed connection time")
	}
	var callbacks int
	c.onRegistration = func(*sessionState, imsgo.RegistrationInfo) { callbacks++ }
	c.syncRegistration("modem-1", 0, client)
	c.syncRegistration("modem-1", 1, &imsgo.Client{})
	if callbacks != 0 {
		t.Fatal("stale client or session published registration")
	}
}

func TestIMSNumberConcurrentAccessUpdates(t *testing.T) {
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1", SIM: &mmodem.SIM{Identifier: "profile-a"}}
	session := &sessionState{id: 1, modem: modem, profileID: "profile-a", numberTarget: modem.Snapshot().SIMIdentity}
	c := &Connectivity{}
	var wg sync.WaitGroup
	for _, access := range []Access{AccessVoLTE, AccessWiFiCalling} {
		wg.Go(func() {
			for range 100 {
				c.updateNumber(access, session, imsgo.RegistrationInfo{Registered: true, MSISDN: "+15551234567"})
				modem.Snapshot()
			}
		})
	}
	wg.Wait()
	if got := modem.Snapshot().Number; got != "+15551234567" {
		t.Fatalf("Number = %q", got)
	}
}

func TestIMSNumberStopAllRetiresRegistration(t *testing.T) {
	connectivity := NewConnectivity(ConnectivityConfig{})
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1", SIM: &mmodem.SIM{Identifier: "profile-a"}}
	session := &sessionState{id: 1, modem: modem, profileID: "profile-a", numberTarget: modem.Snapshot().SIMIdentity}
	connectivity.volte.sessions["modem-1"] = session
	connectivity.updateNumber(AccessVoLTE, session, imsgo.RegistrationInfo{Registered: true, MSISDN: "+15551234567"})
	if _, err := connectivity.volte.stopAllContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	connectivity.updateNumber(AccessWiFiCalling, session, imsgo.RegistrationInfo{Registered: true, MSISDN: "+15557654321"})
	if got := modem.Snapshot().Number; got != "+15557654321" {
		t.Fatalf("number after stop and Wi-Fi registration = %q, want +15557654321", got)
	}
}
