package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	appupdate "github.com/damonto/sigmo/internal/app/update"
)

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
}
