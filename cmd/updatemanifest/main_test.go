package main

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	appupdate "github.com/damonto/sigmo/internal/app/update"
)

func gzipData(t *testing.T, data []byte) []byte {
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
	return compressed.Bytes()
}

func TestRunWritesVerifiableManifest(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	artifact := filepath.Join(dir, "sigmo-linux-amd64")
	if err := os.WriteFile(artifact, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "sigmo-update.json")
	err = run(config{
		edition: "community", channel: "stable", version: "v1.2.3",
		commit: "0123456789abcdef0123456789abcdef01234567", publishedAt: "2026-08-09T12:00:00Z",
		output: output, privateKey: base64.RawStdEncoding.EncodeToString(privateKey),
		artifacts: []string{"linux-amd64=" + artifact},
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	manifestData, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := os.ReadFile(output + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	manifest, verified, err := appupdate.ParseManifest(manifestData, string(signature), base64.RawStdEncoding.EncodeToString(publicKey))
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	if !verified || manifest.Version != "v1.2.3" || len(manifest.Artifacts) != 1 {
		t.Fatalf("ParseManifest() = %+v, verified %v", manifest, verified)
	}
	artifactMetadata := manifest.Artifacts[0]
	if artifactMetadata.Compression != appupdate.ArtifactCompressionNone ||
		artifactMetadata.ExecutableSize != artifactMetadata.Size ||
		artifactMetadata.ExecutableSHA256 != artifactMetadata.SHA256 {
		t.Fatalf("artifact metadata = %+v", artifactMetadata)
	}
}

func TestRunWritesGzipMetadata(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	executable := []byte("compressed executable")
	archive := gzipData(t, executable)
	artifactPath := filepath.Join(dir, "sigmo-pro-linux-amd64.gz")
	if err := os.WriteFile(artifactPath, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "manifest.json")
	if err := run(config{
		edition: "pro", channel: "stable", version: "v1.2.3",
		commit: "0123456789abcdef0123456789abcdef01234567", publishedAt: "2026-08-09T12:00:00Z",
		output: output, privateKey: base64.RawStdEncoding.EncodeToString(privateKey),
		compression: appupdate.ArtifactCompressionGzip,
		artifacts:   []string{"linux-amd64=" + artifactPath},
	}); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	manifestData, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := os.ReadFile(output + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	manifest, verified, err := appupdate.ParseManifest(
		manifestData,
		string(signature),
		base64.RawStdEncoding.EncodeToString(publicKey),
	)
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	if !verified {
		t.Fatal("manifest signature was not verified")
	}
	artifact := manifest.Artifacts[0]
	archiveSum := sha256.Sum256(archive)
	executableSum := sha256.Sum256(executable)
	if artifact.Name != "sigmo-pro-linux-amd64.gz" ||
		artifact.Compression != appupdate.ArtifactCompressionGzip ||
		artifact.Size != int64(len(archive)) ||
		artifact.SHA256 != hex.EncodeToString(archiveSum[:]) ||
		artifact.ExecutableSize != int64(len(executable)) ||
		artifact.ExecutableSHA256 != hex.EncodeToString(executableSum[:]) {
		t.Fatalf("artifact metadata = %+v", artifact)
	}
}

func TestInspectGzipArtifactRejectsInvalidData(t *testing.T) {
	valid := gzipData(t, []byte("binary"))
	tests := []struct {
		name string
		data []byte
	}{
		{name: "invalid", data: []byte("not gzip")},
		{name: "truncated", data: append([]byte(nil), valid[:len(valid)-2]...)},
		{name: "trailing", data: append(append([]byte(nil), valid...), 'x')},
		{name: "multiple members", data: append(append([]byte(nil), valid...), gzipData(t, []byte("second"))...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "artifact.gz")
			if err := os.WriteFile(path, tt.data, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, _, err := inspectGzipArtifact(path); err == nil {
				t.Fatal("inspectGzipArtifact() error = nil")
			}
		})
	}
}
