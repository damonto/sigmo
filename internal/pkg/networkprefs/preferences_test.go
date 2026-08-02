package networkprefs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestStoreIgnoresLegacyPreferences(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	legacy := struct {
		AirplaneMode bool `json:"airplaneMode"`
	}{AirplaneMode: true}
	if err := db.Put(ctx, "modem:modem-1", "network.preferences", legacy); err != nil {
		t.Fatalf("Put() legacy preferences error = %v", err)
	}

	preferences, err := New(db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	enabled, ok, err := preferences.SavedAirplaneMode(ctx, "modem-1")
	if err != nil {
		t.Fatalf("SavedAirplaneMode() error = %v", err)
	}
	if ok || enabled {
		t.Fatalf("SavedAirplaneMode() = %t, %t; want false, false", enabled, ok)
	}
}

func TestStoreSavesV2AirplaneMode(t *testing.T) {
	db := openTestStore(t)
	preferences, err := New(db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := preferences.SaveAirplaneMode(context.Background(), " modem-1 ", true); err != nil {
		t.Fatalf("SaveAirplaneMode() error = %v", err)
	}

	enabled, ok, err := preferences.SavedAirplaneMode(context.Background(), "modem-1")
	if err != nil {
		t.Fatalf("SavedAirplaneMode() error = %v", err)
	}
	if !ok || !enabled {
		t.Fatalf("SavedAirplaneMode() = %t, %t; want true, true", enabled, ok)
	}
}

func TestStorePreservesAutomaticBandSelection(t *testing.T) {
	db := openTestStore(t)
	preferences, err := New(db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := preferences.SaveBands(context.Background(), "modem-1", nil); err != nil {
		t.Fatalf("SaveBands() error = %v", err)
	}

	saved, ok, err := preferences.load(context.Background(), "modem-1")
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if !ok || saved.Bands == nil || len(*saved.Bands) != 0 {
		t.Fatalf("saved bands = %#v, %t; want explicit empty selection", saved.Bands, ok)
	}
}

func openTestStore(t *testing.T) *storage.Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Store.Close() error = %v", err)
		}
	})
	return db
}
