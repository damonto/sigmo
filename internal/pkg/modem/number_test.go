package modem

import (
	"errors"
	"sync"
	"testing"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestIMSNumber(t *testing.T) {
	tests := []struct {
		name string
		own  []string
		ims  string
		want string
	}{
		{name: "missing modem number", ims: "+15551234567", want: "+15551234567"},
		{name: "empty modem record", own: []string{" "}, ims: "+15551234567", want: "+15551234567"},
		{name: "modem takes precedence", own: []string{"+15557654321"}, ims: "+15551234567", want: "+15557654321"},
		{name: "cleared IMS number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Modem{}
			info := wwanmodem.SIMInfo{Slot: 1, ICCID: "profile-a", State: wwanmodem.SIMStateReady, OwnNumbers: tt.own}
			m.applySIMInfo(info)
			target := m.Snapshot().SIMIdentity
			if !m.SetIMSNumber(target, "+15550000000") || !m.SetIMSNumber(target, tt.ims) {
				t.Fatal("SetIMSNumber rejected current SIM")
			}
			m.applySIMInfo(info)
			if got := m.Snapshot().Number; got != tt.want {
				t.Fatalf("Number = %q, want %q", got, tt.want)
			}
			if len(tt.own) == 0 && m.Number != "" {
				t.Fatalf("modem-reported number was overwritten: %q", m.Number)
			}
		})
	}
}

func TestIMSNumberRejectsStaleSIM(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Modem)
	}{
		{name: "profile changed", change: func(m *Modem) { m.applySIMInfo(wwanmodem.SIMInfo{Slot: 1, ICCID: "profile-b"}) }},
		{name: "ICCID missing", change: func(m *Modem) { m.applySIMInfo(wwanmodem.SIMInfo{Slot: 1}) }},
		{name: "SIM absent with stale ICCID", change: func(m *Modem) {
			m.applySIMInfo(wwanmodem.SIMInfo{Slot: 1, ICCID: "profile-a", State: wwanmodem.SIMStateAbsent})
		}},
		{name: "active identity changed", change: func(m *Modem) { m.applyActiveSIMIdentity(1, "profile-b") }},
		{name: "slot changed", change: func(m *Modem) { m.applyActiveSIMIdentity(2, "profile-a") }},
		{name: "slot inventory changed", change: func(m *Modem) { m.applySIMSlots([]wwanmodem.SIMSlot{{Index: 2, Active: true, ICCID: "profile-b"}}) }},
		{name: "slot inventory absent", change: func(m *Modem) {
			m.applySIMSlots([]wwanmodem.SIMSlot{{Index: 1, Active: true, State: wwanmodem.SIMStateAbsent}})
		}},
		{name: "status reports removal", change: func(m *Modem) { m.applyStatus(wwanmodem.Status{SIM: wwanmodem.SIMStateAbsent}) }},
		{name: "return to original profile", change: func(m *Modem) {
			m.applyActiveSIMIdentity(1, "profile-b")
			m.applyActiveSIMIdentity(1, "profile-a")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Modem{}
			m.applySIMInfo(wwanmodem.SIMInfo{Slot: 1, ICCID: "profile-a", State: wwanmodem.SIMStateReady})
			target := m.Snapshot().SIMIdentity
			if !m.SetIMSNumber(target, "+15551234567") {
				t.Fatal("SetIMSNumber rejected current SIM")
			}
			tt.change(m)
			if m.SetIMSNumber(target, "+15551234567") {
				t.Fatal("SetIMSNumber accepted stale SIM")
			}
			if got := m.Snapshot().Number; got != "" {
				t.Fatalf("Number = %q after SIM change", got)
			}
		})
	}
}

func TestIMSNumberRequiresCurrentModemAndICCID(t *testing.T) {
	tests := []struct {
		name string
		sim  *SIM
	}{
		{name: "no SIM"},
		{name: "EID alone", sim: &SIM{EID: "same-euicc"}},
		{name: "different modem instance", sim: &SIM{Identifier: "profile-a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Modem{SIM: tt.sim}
			other := &Modem{SIM: tt.sim}
			if other.SetIMSNumber(m.Snapshot().SIMIdentity, "+15551234567") {
				t.Fatal("SetIMSNumber accepted unknown or foreign identity")
			}
		})
	}
}

func TestIMSNumberConcurrentSIMChange(t *testing.T) {
	m := &Modem{SIM: &SIM{Identifier: "profile-a"}, PrimarySIMSlot: 1}
	target := m.Snapshot().SIMIdentity
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 100 {
			m.SetIMSNumber(target, "+15551234567")
			m.Snapshot()
		}
	})
	wg.Go(func() { m.applyActiveSIMIdentity(1, "profile-b") })
	wg.Wait()
	if got := m.Snapshot().Number; got != "" {
		t.Fatalf("Number = %q after concurrent SIM change", got)
	}
}

func TestIMSNumberRecoversAfterSIMReactivation(t *testing.T) {
	tests := []struct {
		name   string
		update func(*Modem, wwanmodem.SIMState)
	}{
		{name: "SIM watcher", update: func(m *Modem, state wwanmodem.SIMState) {
			previous := m.Snapshot().SIMIdentity
			m.applySIMInfo(wwanmodem.SIMInfo{Slot: 1, ICCID: "profile-a", State: state})
			m.notifySIMChanged(previous)
		}},
		{name: "status watcher", update: func(m *Modem, state wwanmodem.SIMState) {
			m.applyStatus(wwanmodem.Status{SIM: state})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const path = "/sys/devices/modem-1"
			m := simRefreshTestModem(path, 1, false)
			m.applySIMInfo(wwanmodem.SIMInfo{Slot: 1, ICCID: "profile-a", State: wwanmodem.SIMStateReady})
			old := m.Snapshot().SIMIdentity
			if !m.SetIMSNumber(old, "+15551234567") {
				t.Fatal("initial number rejected")
			}
			registry := &Registry{modems: map[string]*Modem{path: m}}
			registry.trackSIMIdentity(m)
			var events []ModemEvent
			registry.subs = []subscription{{fn: func(event ModemEvent) error {
				events = append(events, event)
				return nil
			}}}
			m.onSIMChange = func(slot uint32, iccid string) { registry.publishSIMChanged(m, slot, iccid) }

			tt.update(m, wwanmodem.SIMStateAbsent)
			if len(events) != 1 || events[0].SIMIdentifier != "" {
				t.Fatalf("removal events = %+v, want one event with no active ICCID", events)
			}
			if m.SetIMSNumber(old, "+15551234567") || m.Snapshot().Number != "" {
				t.Fatal("removed SIM retained a number or accepted an old observation")
			}

			tt.update(m, wwanmodem.SIMStateReady)
			if len(events) != 2 || events[1].SIMIdentifier != "profile-a" {
				t.Fatalf("reactivation events = %+v, want a second event for profile-a", events)
			}
			current := events[1].Modem.Snapshot().SIMIdentity
			if current == old || !m.SetIMSNumber(current, "+15557654321") {
				t.Fatal("reactivation did not accept a new observation")
			}
			if m.SetIMSNumber(old, "+15551234567") || m.Snapshot().Number != "+15557654321" {
				t.Fatal("delayed observation overwrote the reactivated SIM number")
			}
			tt.update(m, wwanmodem.SIMStateReady)
			if len(events) != 2 {
				t.Fatalf("unchanged SIM published another event: %+v", events)
			}
		})
	}
}

func TestSIMPresenceWithoutICCIDPublishesChanges(t *testing.T) {
	const path = "/sys/devices/modem-1"
	m := simRefreshTestModem(path, 1, false)
	m.applySIMInfo(wwanmodem.SIMInfo{State: wwanmodem.SIMStateReady})
	registry := &Registry{modems: map[string]*Modem{path: m}}
	registry.trackSIMIdentity(m)
	var events []ModemEvent
	registry.subs = []subscription{{fn: func(event ModemEvent) error {
		events = append(events, event)
		return nil
	}}}
	m.onSIMChange = func(slot uint32, iccid string) { registry.publishSIMChanged(m, slot, iccid) }

	original := m.SIMIdentity()
	m.applyStatus(wwanmodem.Status{SIM: wwanmodem.SIMStateAbsent})
	m.applyStatus(wwanmodem.Status{SIM: wwanmodem.SIMStateReady})
	if current := m.SIMIdentity(); current == original || current.ICCID != "" || current.Slot != 0 {
		t.Fatalf("SIM identity after reinsertion = %+v, original = %+v", current, original)
	}
	if len(events) != 2 {
		t.Fatalf("SIM events = %+v, want one event per presence change", events)
	}
	for _, event := range events {
		if event.Type != ModemEventSIMChanged || event.PreviousSIMSlot != 0 || event.SIMSlot != 0 || event.SIMIdentifier != "" {
			t.Fatalf("SIM presence event = %+v, want unspecified slot and empty ICCID", event)
		}
	}
}

func TestNumberChangeNotifications(t *testing.T) {
	m := &Modem{}
	var observed []ModemSnapshot
	m.onNumberChange = func() { observed = append(observed, m.Snapshot()) }
	info := wwanmodem.SIMInfo{Slot: 1, ICCID: "sim-a", State: wwanmodem.SIMStateReady, OwnNumbers: []string{"+100"}}
	m.applySIMInfo(info)
	target := m.Snapshot().SIMIdentity
	m.applySIMInfo(info)
	if !m.SetIMSNumber(target, "+900") {
		t.Fatal("IMS number rejected")
	}
	if len(observed) != 1 {
		t.Fatalf("unchanged effective number emitted %d notifications", len(observed))
	}
	info.OwnNumbers = []string{"+200"}
	m.applySIMInfo(info)
	m.applySIMInfo(wwanmodem.SIMInfo{Slot: 1, ICCID: "sim-b", State: wwanmodem.SIMStateReady})
	if m.SetIMSNumber(target, "+999") {
		t.Fatal("stale SIM number accepted")
	}
	if !m.SetIMSNumber(m.Snapshot().SIMIdentity, "+300") {
		t.Fatal("new SIM IMS number rejected")
	}
	if len(observed) != 3 {
		t.Fatalf("observations = %+v, want three", observed)
	}
	for i, want := range []struct{ iccid, number string }{{"sim-a", "+100"}, {"sim-a", "+200"}, {"sim-b", "+300"}} {
		if got := observed[i]; got.SIMIdentity.ICCID != want.iccid || got.Number != want.number {
			t.Fatalf("observation %d = %+v, want %+v", i, got, want)
		}
	}
}

func TestNumberChangeCallbackCanUpdateNumber(t *testing.T) {
	m := &Modem{SIM: &SIM{Identifier: "sim-a"}}
	target := m.Snapshot().SIMIdentity
	var wg sync.WaitGroup
	m.onNumberChange = func() {
		if m.Snapshot().Number != "+100" {
			return
		}
		updated := make(chan error, 1)
		wg.Go(func() {
			updated <- m.UpdateStoredNumber(target, func(ModemSnapshot) (string, error) { return "+200", nil })
		})
		select {
		case err := <-updated:
			if err != nil {
				t.Errorf("number update from subscriber: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("number update blocked until subscriber returned")
		}
	}
	if !m.SetIMSNumber(target, "+100") {
		t.Fatal("initial number rejected")
	}
	// Even a failed assertion lets the subscriber return and release its lock.
	wg.Wait()
	if got := m.Snapshot().Number; got != "+200" {
		t.Fatalf("number = %q, want subscriber's update", got)
	}
}

func TestStoredNumberPriorityAndActivation(t *testing.T) {
	m := &Modem{}
	info := wwanmodem.SIMInfo{Slot: 1, ICCID: "sim-a", State: wwanmodem.SIMStateReady, OwnNumbers: []string{"+100"}}
	m.applySIMInfo(info)
	target := m.SIMIdentity()
	store := func(ModemSnapshot) (string, error) { return "+200", nil }
	if err := m.UpdateStoredNumber(target, store); err != nil {
		t.Fatal(err)
	}
	m.SetIMSNumber(target, "+300")
	info.OwnNumbers = []string{"+400"}
	m.applySIMInfo(info)
	if got := m.Snapshot(); got.Number != "+200" || got.CardNumber != "+400" {
		t.Fatalf("snapshot number = %q, card number = %q", got.Number, got.CardNumber)
	}
	m.InvalidateSIMActivation()
	if err := m.UpdateStoredNumber(target, store); !errors.Is(err, ErrSIMActivationChanged) {
		t.Fatalf("stale number update = %v", err)
	}
	info.ICCID, info.OwnNumbers = "sim-b", nil
	m.applySIMInfo(info)
	if got := m.Snapshot().Number; got != "" {
		t.Fatalf("replacement SIM inherited %q", got)
	}
}
