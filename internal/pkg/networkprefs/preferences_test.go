package networkprefs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestStoreSavesAirplaneMode(t *testing.T) {
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

func TestStoreRejectsEmptyBandSelection(t *testing.T) {
	db := openTestStore(t)
	preferences, err := New(db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := preferences.SaveBands(context.Background(), "modem-1", nil); err == nil {
		t.Fatal("SaveBands() error = nil, want non-nil")
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
