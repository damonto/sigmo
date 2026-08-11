package update

import (
	"bytes"
	"compress/gzip"

	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRawArtifact(data []byte) Artifact {
	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])
	return Artifact{
		Compression:      ArtifactCompressionNone,
		Size:             int64(len(data)),
		SHA256:           checksum,
		ExecutableSize:   int64(len(data)),
		ExecutableSHA256: checksum,
	}
}

func testGzipArtifact(t *testing.T, data []byte) (Artifact, []byte) {
	t.Helper()
	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	archive := compressed.Bytes()
	archiveSum := sha256.Sum256(archive)
	executableSum := sha256.Sum256(data)
	return Artifact{
		Compression:      ArtifactCompressionGzip,
		Size:             int64(len(archive)),
		SHA256:           hex.EncodeToString(archiveSum[:]),
		ExecutableSize:   int64(len(data)),
		ExecutableSHA256: hex.EncodeToString(executableSum[:]),
	}, archive
}

func setTestArchiveMetadata(artifact Artifact, archive []byte) Artifact {
	sum := sha256.Sum256(archive)
	artifact.Size = int64(len(archive))
	artifact.SHA256 = hex.EncodeToString(sum[:])
	return artifact
}

func TestApplyAndPendingRecovery(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "sigmo")
	oldBinary := []byte("#!/bin/sh\necho v1.0.0\n")
	newBinary := []byte("#!/bin/sh\necho v2.0.0\n")
	if err := os.WriteFile(executable, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	release := Release{
		Verified: true,
		Manifest: Manifest{Version: "v2.0.0"},
		Artifact: testRawArtifact(newBinary),
	}
	if err := Apply(t.Context(), executable, release, bytes.NewReader(newBinary)); err != nil {
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
	if err := Apply(t.Context(), executable, Release{
		Verified: true, Manifest: Manifest{Version: "v2.0.0"},
		Artifact: testRawArtifact(newBinary),
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
	if err := Apply(t.Context(), executable, Release{
		Verified: true, Manifest: Manifest{Version: "v2.0.0"},
		Artifact: testRawArtifact(newBinary),
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
			artifact := testRawArtifact([]byte("binary"))
			artifact.Size = tt.size
			artifact.SHA256 = tt.checksum
			err := Apply(t.Context(), executable, Release{
				Verified: true,
				Manifest: Manifest{Version: "v2.0.0"},
				Artifact: artifact,
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

func TestApplyGzipArtifact(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "sigmo")
	oldBinary := []byte("#!/bin/sh\necho v1.0.0\n")
	newBinary := []byte("#!/bin/sh\necho v2.0.0\n")
	if err := os.WriteFile(executable, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	artifact, archive := testGzipArtifact(t, newBinary)
	if err := Apply(t.Context(), executable, Release{
		Verified: true,
		Manifest: Manifest{Version: "v2.0.0"},
		Artifact: artifact,
	}, bytes.NewReader(archive)); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	got, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newBinary) {
		t.Fatalf("installed executable = %q", got)
	}
}

func TestApplyRejectsInvalidGzipArtifact(t *testing.T) {
	newBinary := []byte("#!/bin/sh\necho v2.0.0\n")
	baseArtifact, baseArchive := testGzipArtifact(t, newBinary)
	_, secondArchive := testGzipArtifact(t, []byte("second member"))
	truncated := append([]byte(nil), baseArchive[:len(baseArchive)-2]...)
	invalid := []byte("not a gzip stream")
	trailing := append(append([]byte(nil), baseArchive...), 'x')
	multiple := append(append([]byte(nil), baseArchive...), secondArchive...)

	tests := []struct {
		name     string
		body     []byte
		artifact Artifact
	}{
		{
			name: "compressed size", body: baseArchive,
			artifact: func() Artifact {
				artifact := baseArtifact
				artifact.Size--
				return artifact
			}(),
		},
		{
			name: "compressed checksum", body: baseArchive,
			artifact: func() Artifact {
				artifact := baseArtifact
				artifact.SHA256 = strings.Repeat("0", 64)
				return artifact
			}(),
		},
		{
			name: "executable size", body: baseArchive,
			artifact: func() Artifact {
				artifact := baseArtifact
				artifact.ExecutableSize--
				return artifact
			}(),
		},
		{
			name: "executable checksum", body: baseArchive,
			artifact: func() Artifact {
				artifact := baseArtifact
				artifact.ExecutableSHA256 = strings.Repeat("0", 64)
				return artifact
			}(),
		},
		{name: "truncated", body: truncated, artifact: setTestArchiveMetadata(baseArtifact, truncated)},
		{name: "invalid", body: invalid, artifact: setTestArchiveMetadata(baseArtifact, invalid)},
		{name: "trailing", body: trailing, artifact: setTestArchiveMetadata(baseArtifact, trailing)},
		{name: "multiple members", body: multiple, artifact: setTestArchiveMetadata(baseArtifact, multiple)},
		{
			name: "unknown compression", body: baseArchive,
			artifact: func() Artifact {
				artifact := baseArtifact
				artifact.Compression = "zstd"
				return artifact
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "sigmo")
			oldBinary := []byte("#!/bin/sh\necho v1.0.0\n")
			if err := os.WriteFile(executable, oldBinary, 0o755); err != nil {
				t.Fatal(err)
			}
			err := Apply(t.Context(), executable, Release{
				Verified: true,
				Manifest: Manifest{Version: "v2.0.0"},
				Artifact: tt.artifact,
			}, bytes.NewReader(tt.body))
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
