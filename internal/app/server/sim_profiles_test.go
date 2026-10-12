package server

import (
	"context"
	"maps"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestSIMProfileSubscription(t *testing.T) {
	ctx := t.Context()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "profiles.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	current := &modem.Modem{EquipmentIdentifier: "modem", SIM: &modem.SIM{Identifier: "sim-a"}, Number: "+100"}
	registry := &profileRegistry{present: map[string]*modem.Modem{"modem": current}}
	unsubscribe, err := subscribeSIMProfiles(ctx, registry, store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unsubscribe)
	assertNumbers := func(want map[string]string) {
		t.Helper()
		got, err := store.SIMProfileNumbers(ctx, []string{"sim-a", "sim-b"})
		if err != nil || !maps.Equal(got, want) {
			t.Fatalf("numbers = %v, want %v, error = %v", got, want, err)
		}
	}
	assertNumbers(map[string]string{"sim-a": "+100"})
	if !current.SetNumber(current.Snapshot().SIMIdentity, "+200") {
		t.Fatal("number update rejected")
	}
	if err := registry.notify(modem.ModemEvent{Type: modem.ModemEventNumberChanged, Modem: current}); err != nil {
		t.Fatal(err)
	}
	assertNumbers(map[string]string{"sim-a": "+200"})

	// A different SIM on the same physical device retains the previous SIM's number.
	replacement := &modem.Modem{EquipmentIdentifier: "modem", SIM: &modem.SIM{Identifier: "sim-b"}}
	registry.present["modem"] = replacement
	if err := registry.notify(modem.ModemEvent{Type: modem.ModemEventSIMChanged, Modem: replacement}); err != nil {
		t.Fatal(err)
	}
	assertNumbers(map[string]string{"sim-a": "+200", "sim-b": ""})
	if !current.SetNumber(current.Snapshot().SIMIdentity, "+999") {
		t.Fatal("old modem observation rejected before registry validation")
	}
	if !replacement.SetFallbackNumber(replacement.Snapshot().SIMIdentity, "+300") {
		t.Fatal("IMS number rejected")
	}
	for _, event := range []modem.ModemEvent{
		{Type: modem.ModemEventChanged, Modem: replacement},
		{Type: modem.ModemEventNumberChanged, Modem: current},
	} {
		if err := registry.notify(event); err != nil {
			t.Fatal(err)
		}
	}
	assertNumbers(map[string]string{"sim-a": "+200", "sim-b": "+300"})
	if !replacement.SetFallbackNumber(replacement.Snapshot().SIMIdentity, "") {
		t.Fatal("clearing IMS number rejected")
	}
	if err := registry.notify(modem.ModemEvent{Type: modem.ModemEventNumberChanged, Modem: replacement}); err != nil {
		t.Fatal(err)
	}
	assertNumbers(map[string]string{"sim-a": "+200", "sim-b": "+300"})

	// Callbacks can arrive concurrently and after a newer number is already set.
	// The last stored observation must agree with the current modem snapshot.
	target := replacement.Snapshot().SIMIdentity
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if !replacement.SetFallbackNumber(target, "+400"+strconv.Itoa(i)) {
				t.Error("concurrent number update rejected")
				return
			}
			if err := registry.notify(modem.ModemEvent{Type: modem.ModemEventNumberChanged, Modem: replacement}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	assertNumbers(map[string]string{"sim-a": "+200", "sim-b": replacement.Snapshot().Number})
	unsubscribe()
	if registry.notify != nil {
		t.Fatal("subscription remained after cleanup")
	}
}

type profileRegistry struct {
	present map[string]*modem.Modem
	notify  func(modem.ModemEvent) error
}

func (r *profileRegistry) Modems(context.Context) (map[string]*modem.Modem, error) {
	return maps.Clone(r.present), nil
}

func (r *profileRegistry) Subscribe(_ context.Context, notify func(modem.ModemEvent) error) (func(), error) {
	r.notify = notify
	return func() { r.notify = nil }, nil
}
