package storage

import (
	"path/filepath"
	"testing"
	"time"

	sqlite3driver "github.com/ncruces/go-sqlite3/driver"
)

func TestInternetSpeedTestSurvivesDatabaseUpgradeAndRestart(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "sigmo.db")
	db, err := sqlite3driver.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newMigrationProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 13); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	const iccid = "8986000000000000001"
	if got, err := store.LatestInternetSpeedTest(ctx, iccid); err != nil || got != nil {
		t.Fatalf("initial result = %+v, %v", got, err)
	}
	result := InternetSpeedTest{DownloadBPS: 1_234_567, UploadBPS: 456_789, TestedAt: time.UnixMilli(1234567).UTC()}
	if err := store.SaveInternetSpeedTest(ctx, iccid, result); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if got, err := store.LatestInternetSpeedTest(ctx, iccid); err != nil || got == nil || *got != result {
		t.Fatalf("saved result = %+v, %v, want %+v", got, err, result)
	}
}
