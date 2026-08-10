package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

func TestParseManifestSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Edition:       "community",
		Channel:       "stable",
		Version:       "v1.2.3",
		Commit:        testCommit,
		PublishedAt:   time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		Artifacts: []Artifact{{
			Target: "linux-amd64", Name: "sigmo-linux-amd64", Compression: ArtifactCompressionNone,
			Size: 6, SHA256: "9a3a45d01531a20e89ac6ae10b0b0beb0492acd7216a368aa062d1a5fecaf9cd",
			ExecutableSize: 6, ExecutableSHA256: "9a3a45d01531a20e89ac6ae10b0b0beb0492acd7216a368aa062d1a5fecaf9cd",
		}},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, data))
	key := base64.RawStdEncoding.EncodeToString(publicKey)

	parsed, verified, err := ParseManifest(data, signature, key)
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	if !verified || parsed.Version != manifest.Version {
		t.Fatalf("ParseManifest() = %+v, verified %v", parsed, verified)
	}
	if _, _, err := ParseManifest(data, base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)), key); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("ParseManifest() bad signature error = %v", err)
	}
	if _, verified, err := ParseManifest(data, "", ""); err != nil || verified {
		t.Fatalf("ParseManifest() unsigned = verified %v, error %v", verified, err)
	}
}

func TestValidateManifestRejectsInvalidReleaseIdentity(t *testing.T) {
	base := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Edition:       "community",
		Channel:       "stable",
		Version:       "v1.2.3",
		Commit:        testCommit,
		PublishedAt:   time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		Artifacts: []Artifact{{
			Target: "linux-amd64", Name: "sigmo-linux-amd64", Compression: ArtifactCompressionNone,
			Size: 6, SHA256: "9a3a45d01531a20e89ac6ae10b0b0beb0492acd7216a368aa062d1a5fecaf9cd",
			ExecutableSize: 6, ExecutableSHA256: "9a3a45d01531a20e89ac6ae10b0b0beb0492acd7216a368aa062d1a5fecaf9cd",
		}},
	}
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{name: "edition", mutate: func(manifest *Manifest) { manifest.Edition = "enterprise" }},
		{name: "channel", mutate: func(manifest *Manifest) { manifest.Channel = "nightly" }},
		{name: "short commit", mutate: func(manifest *Manifest) { manifest.Commit = "01234567" }},
		{name: "uppercase commit", mutate: func(manifest *Manifest) { manifest.Commit = strings.ToUpper(testCommit) }},
		{name: "stable version", mutate: func(manifest *Manifest) { manifest.Version = "v1.2" }},
		{name: "stable prerelease", mutate: func(manifest *Manifest) { manifest.Version = "v1.2.3-rc.1" }},
		{name: "artifact path", mutate: func(manifest *Manifest) { manifest.Artifacts[0].Name = "../sigmo" }},
		{
			name: "dev version mismatch",
			mutate: func(manifest *Manifest) {
				manifest.Channel = "dev"
				manifest.Version = "dev-deadbeef"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := base
			manifest.Artifacts = append([]Artifact(nil), base.Artifacts...)
			tt.mutate(&manifest)
			if err := ValidateManifest(manifest); !errors.Is(err, ErrInvalidManifest) {
				t.Fatalf("ValidateManifest() error = %v", err)
			}
		})
	}
}

func TestFindArtifactRejectsUnknownTarget(t *testing.T) {
	_, err := FindArtifact(Manifest{Artifacts: []Artifact{{Target: "linux-arm64"}}}, "linux-amd64")
	if !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("FindArtifact() error = %v", err)
	}
}

func TestValidateManifestArtifactCompression(t *testing.T) {
	checksum := "9a3a45d01531a20e89ac6ae10b0b0beb0492acd7216a368aa062d1a5fecaf9cd"
	community := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Edition:       "community",
		Channel:       "stable",
		Version:       "v1.2.3",
		Commit:        testCommit,
		PublishedAt:   time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		Artifacts: []Artifact{{
			Target: "linux-amd64", Name: "sigmo-linux-amd64", Compression: ArtifactCompressionNone,
			Size: 6, SHA256: checksum, ExecutableSize: 6, ExecutableSHA256: checksum,
		}},
	}
	pro := community
	pro.Edition = "pro"
	pro.Artifacts = []Artifact{{
		Target: "linux-amd64", Name: "sigmo-pro-linux-amd64.gz", Compression: ArtifactCompressionGzip,
		Size: 5, SHA256: checksum, ExecutableSize: 6, ExecutableSHA256: checksum,
	}}
	if err := ValidateManifest(community); err != nil {
		t.Fatalf("ValidateManifest(community) error = %v", err)
	}
	if err := ValidateManifest(pro); err != nil {
		t.Fatalf("ValidateManifest(pro) error = %v", err)
	}

	tests := []struct {
		name     string
		manifest Manifest
		mutate   func(*Artifact)
	}{
		{
			name: "community gzip", manifest: community,
			mutate: func(artifact *Artifact) { artifact.Compression = ArtifactCompressionGzip },
		},
		{
			name: "community executable size", manifest: community,
			mutate: func(artifact *Artifact) { artifact.ExecutableSize++ },
		},
		{
			name: "community executable checksum", manifest: community,
			mutate: func(artifact *Artifact) { artifact.ExecutableSHA256 = strings.Repeat("0", 64) },
		},
		{
			name: "pro uncompressed", manifest: pro,
			mutate: func(artifact *Artifact) { artifact.Compression = ArtifactCompressionNone },
		},
		{
			name: "gzip suffix", manifest: pro,
			mutate: func(artifact *Artifact) { artifact.Name = "sigmo-pro-linux-amd64" },
		},
		{
			name: "unknown compression", manifest: pro,
			mutate: func(artifact *Artifact) { artifact.Compression = "zstd" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := tt.manifest
			manifest.Artifacts = append([]Artifact(nil), tt.manifest.Artifacts...)
			tt.mutate(&manifest.Artifacts[0])
			if err := ValidateManifest(manifest); !errors.Is(err, ErrInvalidManifest) {
				t.Fatalf("ValidateManifest() error = %v", err)
			}
		})
	}
}
