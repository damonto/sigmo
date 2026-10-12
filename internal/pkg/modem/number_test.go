package modem

import (
	"sync"
	"testing"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestFallbackNumber(t *testing.T) {
	tests := []struct {
		name     string
		own      []string
		fallback string
		want     string
	}{
		{name: "missing modem number", fallback: "+15551234567", want: "+15551234567"},
		{name: "empty modem record", own: []string{" "}, fallback: "+15551234567", want: "+15551234567"},
		{name: "modem takes precedence", own: []string{"+15557654321"}, fallback: "+15551234567", want: "+15557654321"},
		{name: "cleared fallback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Modem{}
			info := wwanmodem.SIMInfo{Slot: 1, ICCID: "profile-a", State: wwanmodem.SIMStateReady, OwnNumbers: tt.own}
			m.applySIMInfo(info)
			target := m.Snapshot().SIMIdentity
			if !m.SetFallbackNumber(target, "+15550000000") || !m.SetFallbackNumber(target, tt.fallback) {
				t.Fatal("SetFallbackNumber rejected current SIM")
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

func TestFallbackNumberRejectsStaleSIM(t *testing.T) {
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
			if !m.SetFallbackNumber(target, "+15551234567") {
				t.Fatal("SetFallbackNumber rejected current SIM")
			}
			tt.change(m)
			if m.SetFallbackNumber(target, "+15551234567") {
				t.Fatal("SetFallbackNumber accepted stale SIM")
			}
			if got := m.Snapshot().Number; got != "" {
				t.Fatalf("Number = %q after SIM change", got)
			}
		})
	}
}

func TestFallbackNumberRequiresCurrentModemAndICCID(t *testing.T) {
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
			if other.SetFallbackNumber(m.Snapshot().SIMIdentity, "+15551234567") {
				t.Fatal("SetFallbackNumber accepted unknown or foreign identity")
			}
		})
	}
}

func TestFallbackNumberConcurrentSIMChange(t *testing.T) {
	m := &Modem{SIM: &SIM{Identifier: "profile-a"}, PrimarySIMSlot: 1}
	target := m.Snapshot().SIMIdentity
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 100 {
			m.SetFallbackNumber(target, "+15551234567")
			m.Snapshot()
		}
	})
	wg.Go(func() { m.applyActiveSIMIdentity(1, "profile-b") })
	wg.Wait()
	if got := m.Snapshot().Number; got != "" {
		t.Fatalf("Number = %q after concurrent SIM change", got)
	}
}

func TestFallbackNumberRecoversAfterSIMReactivation(t *testing.T) {
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
			if !m.SetFallbackNumber(old, "+15551234567") {
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
			if m.SetFallbackNumber(old, "+15551234567") || m.Snapshot().Number != "" {
				t.Fatal("removed SIM retained a number or accepted an old observation")
			}

			tt.update(m, wwanmodem.SIMStateReady)
			if len(events) != 2 || events[1].SIMIdentifier != "profile-a" {
				t.Fatalf("reactivation events = %+v, want a second event for profile-a", events)
			}
			current := events[1].Modem.Snapshot().SIMIdentity
			if current == old || !m.SetFallbackNumber(current, "+15557654321") {
				t.Fatal("reactivation did not accept a new observation")
			}
			if m.SetFallbackNumber(old, "+15551234567") || m.Snapshot().Number != "+15557654321" {
				t.Fatal("delayed observation overwrote the reactivated SIM number")
			}
			tt.update(m, wwanmodem.SIMStateReady)
			if len(events) != 2 {
				t.Fatalf("unchanged SIM published another event: %+v", events)
			}
		})
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
	if !m.SetFallbackNumber(target, "+900") {
		t.Fatal("fallback rejected")
	}
	if len(observed) != 1 {
		t.Fatalf("unchanged effective number emitted %d notifications", len(observed))
	}
	if !m.SetNumber(target, "+200") {
		t.Fatal("manual number rejected")
	}
	m.applySIMInfo(wwanmodem.SIMInfo{Slot: 1, ICCID: "sim-b", State: wwanmodem.SIMStateReady})
	if m.SetNumber(target, "+999") || m.SetFallbackNumber(target, "+999") {
		t.Fatal("stale SIM number accepted")
	}
	if !m.SetFallbackNumber(m.Snapshot().SIMIdentity, "+300") {
		t.Fatal("new SIM fallback rejected")
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
		updated := make(chan bool, 1)
		wg.Go(func() { updated <- m.SetNumber(target, "+200") })
		select {
		case accepted := <-updated:
			if !accepted {
				t.Error("number update from subscriber rejected")
			}
		case <-time.After(time.Second):
			t.Error("number update blocked until subscriber returned")
		}
	}
	if !m.SetNumber(target, "+100") {
		t.Fatal("initial number rejected")
	}
	// Even a failed assertion lets the subscriber return and release its lock.
	wg.Wait()
	if got := m.Snapshot().Number; got != "+200" {
		t.Fatalf("number = %q, want subscriber's update", got)
	}
}
