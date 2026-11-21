package server

import (
	"context"
	"maps"
	"path/filepath"
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
	if err := store.SaveSIMProfileNumber(ctx, "sim-a", "modem", "+200"); err != nil {
		t.Fatal(err)
	}
	if err := registry.notify(modem.ModemEvent{Type: modem.ModemEventNumberChanged, Modem: current}); err != nil {
		t.Fatal(err)
	}
	assertNumbers(map[string]string{"sim-a": "+200"})
	if current.Snapshot().Number != "+200" {
		t.Fatal("the card number overrode the saved number")
	}

	// A newly discovered modem must restore the same saved value after restart.
	restarted := &modem.Modem{EquipmentIdentifier: "modem", SIM: &modem.SIM{Identifier: "sim-a"}, Number: "+100"}
	registry.present["modem"] = restarted
	if err := registry.notify(modem.ModemEvent{Type: modem.ModemEventChanged, Modem: restarted}); err != nil {
		t.Fatal(err)
	}
	if restarted.Snapshot().Number != "+200" {
		t.Fatal("restart did not restore the saved number")
	}

	replacement := &modem.Modem{EquipmentIdentifier: "modem", SIM: &modem.SIM{Identifier: "sim-b"}}
	registry.present["modem"] = replacement
	for _, event := range []modem.ModemEvent{
		{Type: modem.ModemEventSIMChanged, Modem: replacement},
		{Type: modem.ModemEventNumberChanged, Modem: current},
	} {
		if err := registry.notify(event); err != nil {
			t.Fatal(err)
		}
	}
	assertNumbers(map[string]string{"sim-a": "+200", "sim-b": ""})
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
