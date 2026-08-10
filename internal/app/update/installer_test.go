package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyAndPendingRecovery(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "sigmo")
	oldBinary := []byte("#!/bin/sh\necho v1.0.0\n")
	newBinary := []byte("#!/bin/sh\necho v2.0.0\n")
	if err := os.WriteFile(executable, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(newBinary)
	release := Release{
		Verified: true,
		Manifest: Manifest{Version: "v2.0.0"},
		Artifact: Artifact{Size: int64(len(newBinary)), SHA256: hex.EncodeToString(sum[:])},
	}
	if err := Apply(context.Background(), executable, release, bytes.NewReader(newBinary)); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if restored, err := RecoverPending(executable); err != nil || restored {
		t.Fatalf("RecoverPending() first = %v, %v", restored, err)
	}
	if restored, err := RecoverPending(executable); err != nil || !restored {
		t.Fatalf("RecoverPending() second = %v, %v", restored, err)
	}
	got, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, oldBinary) {
		t.Fatalf("restored executable = %q", got)
	}
}

func TestMarkHealthyRemovesBackup(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "sigmo")
	oldBinary := []byte("#!/bin/sh\necho v1.0.0\n")
	newBinary := []byte("#!/bin/sh\necho v2.0.0\n")
	if err := os.WriteFile(executable, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(newBinary)
	if err := Apply(context.Background(), executable, Release{
		Verified: true, Manifest: Manifest{Version: "v2.0.0"},
		Artifact: Artifact{Size: int64(len(newBinary)), SHA256: hex.EncodeToString(sum[:])},
	}, bytes.NewReader(newBinary)); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverPending(executable); err != nil {
		t.Fatal(err)
	}
	if err := MarkHealthy(executable); err != nil {
		t.Fatalf("MarkHealthy() error = %v", err)
	}
	for _, path := range []string{executable + ".previous", markerPath(executable)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists", path)
		}
	}
}

func TestMarkHealthyIgnoresUpdateNotStartedYet(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "sigmo")
	oldBinary := []byte("#!/bin/sh\necho v1.0.0\n")
	newBinary := []byte("#!/bin/sh\necho v2.0.0\n")
	if err := os.WriteFile(executable, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(newBinary)
	if err := Apply(context.Background(), executable, Release{
		Verified: true, Manifest: Manifest{Version: "v2.0.0"},
		Artifact: Artifact{Size: int64(len(newBinary)), SHA256: hex.EncodeToString(sum[:])},
	}, bytes.NewReader(newBinary)); err != nil {
		t.Fatal(err)
	}

	if err := MarkHealthy(executable); err != nil {
		t.Fatalf("MarkHealthy() error = %v", err)
	}
	for _, path := range []string{executable + ".previous", markerPath(executable)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("pending update state %s was removed: %v", path, err)
		}
	}
}

func TestPendingUpdateRejectsUnexpectedBackupPath(t *testing.T) {
	tests := []struct {
		name string
		run  func(string) error
	}{
		{
			name: "recover",
			run: func(executable string) error {
				_, err := RecoverPending(executable)
				return err
			},
		},
		{name: "mark healthy", run: MarkHealthy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "sigmo")
			if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := writePending(markerPath(executable), pendingUpdate{
				Backup:   filepath.Join(dir, "unrelated"),
				Attempts: 1,
			}); err != nil {
				t.Fatal(err)
			}
			if err := tt.run(executable); err == nil {
				t.Fatal("operation error = nil")
			}
			if _, err := os.Stat(executable); err != nil {
				t.Fatalf("current executable was changed: %v", err)
			}
		})
	}
}

func TestWritePendingUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sigmo.update-pending")
	if err := writePending(path, pendingUpdate{Backup: "/tmp/sigmo.previous"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("marker permissions = %o, want 600", got)
	}
}

func TestApplyKeepsCurrentExecutableOnValidationFailure(t *testing.T) {
	tests := []struct {
		name     string
		size     int64
		checksum string
	}{
		{name: "size", size: 1, checksum: strings.Repeat("0", 64)},
		{name: "checksum", size: 6, checksum: strings.Repeat("0", 64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "sigmo")
			oldBinary := []byte("#!/bin/sh\necho v1.0.0\n")
			if err := os.WriteFile(executable, oldBinary, 0o755); err != nil {
				t.Fatal(err)
			}
			err := Apply(context.Background(), executable, Release{
				Verified: true,
				Manifest: Manifest{Version: "v2.0.0"},
				Artifact: Artifact{Size: tt.size, SHA256: tt.checksum},
			}, bytes.NewReader([]byte("binary")))
			if err == nil {
				t.Fatal("Apply() error = nil")
			}
			got, readErr := os.ReadFile(executable)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(got, oldBinary) {
				t.Fatalf("current executable = %q", got)
			}
			for _, path := range []string{markerPath(executable), executable + ".previous"} {
				if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("unexpected update state %s: %v", path, statErr)
				}
			}
		})
	}
}
