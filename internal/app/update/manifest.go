package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

var (
	ErrInvalidManifest  = errors.New("invalid update manifest")
	ErrInvalidSignature = errors.New("invalid update manifest signature")
)

func ParseManifest(data []byte, signature string, publicKey string) (Manifest, bool, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, false, fmt.Errorf("decode update manifest: %w", err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, false, err
	}
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return manifest, false, nil
	}
	key, err := decodeBase64(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return Manifest{}, false, fmt.Errorf("decode release public key: %w", ErrInvalidSignature)
	}
	sig, err := decodeBase64(strings.TrimSpace(signature))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(key), data, sig) {
		return Manifest{}, false, ErrInvalidSignature
	}
	return manifest, true, nil
}

func FindArtifact(manifest Manifest, target string) (Artifact, error) {
	index := slices.IndexFunc(manifest.Artifacts, func(artifact Artifact) bool {
		return artifact.Target == target
	})
	if index < 0 {
		return Artifact{}, fmt.Errorf("%w: target %q is unavailable", ErrInvalidManifest, target)
	}
	return manifest.Artifacts[index], nil
}

func ValidateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("%w: schema version %d", ErrInvalidManifest, manifest.SchemaVersion)
	}
	if manifest.Edition != "community" && manifest.Edition != "pro" {
		return fmt.Errorf("%w: edition must be community or pro", ErrInvalidManifest)
	}
	if manifest.Channel != "stable" && manifest.Channel != "dev" {
		return fmt.Errorf("%w: channel must be stable or dev", ErrInvalidManifest)
	}
	if !validCommit(manifest.Commit) {
		return fmt.Errorf("%w: commit must be a full Git SHA", ErrInvalidManifest)
	}
	switch manifest.Channel {
	case "stable":
		if !validStableVersion(manifest.Version) {
			return fmt.Errorf("%w: stable version must be a semantic vMAJOR.MINOR.PATCH tag", ErrInvalidManifest)
		}
	case "dev":
		want := "dev-" + strings.ToLower(manifest.Commit[:8])
		if !strings.EqualFold(manifest.Version, want) {
			return fmt.Errorf("%w: dev version must match %s", ErrInvalidManifest, want)
		}
	}
	if manifest.PublishedAt.IsZero() || len(manifest.Artifacts) == 0 {
		return fmt.Errorf("%w: publication time and artifacts are required", ErrInvalidManifest)
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		if strings.TrimSpace(artifact.Target) != artifact.Target || artifact.Target == "" ||
			strings.TrimSpace(artifact.Name) != artifact.Name || artifact.Name == "" ||
			artifact.Size <= 0 || artifact.ExecutableSize <= 0 {
			return fmt.Errorf("%w: incomplete artifact", ErrInvalidManifest)
		}
		if filepath.Base(artifact.Name) != artifact.Name {
			return fmt.Errorf("%w: artifact name %q must not contain a path", ErrInvalidManifest, artifact.Name)
		}
		if !validSHA256(artifact.SHA256) {
			return fmt.Errorf("%w: invalid sha256 for %s", ErrInvalidManifest, artifact.Target)
		}
		if !validSHA256(artifact.ExecutableSHA256) {
			return fmt.Errorf("%w: invalid executable sha256 for %s", ErrInvalidManifest, artifact.Target)
		}
		switch artifact.Compression {
		case ArtifactCompressionNone:
			if manifest.Edition != "community" {
				return fmt.Errorf("%w: Pro artifacts must use gzip compression", ErrInvalidManifest)
			}
			if artifact.Size != artifact.ExecutableSize || !strings.EqualFold(artifact.SHA256, artifact.ExecutableSHA256) {
				return fmt.Errorf("%w: uncompressed artifact metadata must match the executable", ErrInvalidManifest)
			}
		case ArtifactCompressionGzip:
			if manifest.Edition != "pro" {
				return fmt.Errorf("%w: Community artifacts must not be compressed", ErrInvalidManifest)
			}
			if !strings.HasSuffix(artifact.Name, ".gz") {
				return fmt.Errorf("%w: gzip artifact name %q must end in .gz", ErrInvalidManifest, artifact.Name)
			}
		default:
			return fmt.Errorf("%w: unsupported artifact compression %q", ErrInvalidManifest, artifact.Compression)
		}
		if _, ok := seen[artifact.Target]; ok {
			return fmt.Errorf("%w: duplicate target %q", ErrInvalidManifest, artifact.Target)
		}
		seen[artifact.Target] = struct{}{}
	}
	return nil
}

func validSHA256(value string) bool {
	checksum, err := hex.DecodeString(value)
	return err == nil && len(checksum) == sha256.Size
}

func validStableVersion(version string) bool {
	return semver.IsValid(version) &&
		!strings.ContainsAny(version, "-+") &&
		len(strings.Split(strings.TrimPrefix(version, "v"), ".")) == 3
}

func validCommit(commit string) bool {
	if len(commit) != 40 || commit != strings.ToLower(commit) {
		return false
	}
	_, err := hex.DecodeString(commit)
	return err == nil
}

func decodeBase64(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding, base64.RawURLEncoding, base64.URLEncoding} {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid base64")
}
