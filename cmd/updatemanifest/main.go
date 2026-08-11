package main

import (
	"bufio"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	appupdate "github.com/damonto/sigmo/internal/app/update"
)

type artifactFlags []string

func (f *artifactFlags) String() string { return strings.Join(*f, ",") }
func (f *artifactFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func main() {
	var artifacts artifactFlags
	var edition, channel, version, commit, notes, notesFile, output, privateKeyEnv, compression string
	var publishedAt string
	flag.StringVar(&edition, "edition", "", "community or pro")
	flag.StringVar(&channel, "channel", "", "stable or dev")
	flag.StringVar(&version, "version", "", "release version")
	flag.StringVar(&commit, "commit", "", "full Git commit SHA")
	flag.StringVar(&notes, "notes", "", "release notes")
	flag.StringVar(&notesFile, "notes-file", "", "file containing release notes")
	flag.StringVar(&publishedAt, "published-at", "", "RFC3339 publication time; defaults to now")
	flag.StringVar(&output, "output", "sigmo-update.json", "manifest output path")
	flag.StringVar(&privateKeyEnv, "private-key-env", "SIGMO_RELEASE_PRIVATE_KEY", "environment variable containing the signing key")
	flag.StringVar(&compression, "compression", appupdate.ArtifactCompressionNone, "artifact compression: none or gzip")
	flag.Var(&artifacts, "artifact", "target=path; repeat for every artifact")
	flag.Parse()

	if err := run(config{
		edition: edition, channel: channel, version: version, commit: commit,
		notes: notes, notesFile: notesFile, publishedAt: publishedAt,
		output: output, privateKey: os.Getenv(privateKeyEnv), compression: compression, artifacts: artifacts,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type config struct {
	edition, channel, version, commit string
	notes, notesFile, publishedAt     string
	output, privateKey                string
	compression                       string
	artifacts                         []string
}

func run(cfg config) error {
	if cfg.edition == "" || cfg.channel == "" || cfg.version == "" || cfg.commit == "" {
		return errors.New("edition, channel, version, and commit are required")
	}
	if len(cfg.artifacts) == 0 {
		return errors.New("at least one artifact is required")
	}
	if cfg.compression == "" {
		cfg.compression = appupdate.ArtifactCompressionNone
	}
	if cfg.compression != appupdate.ArtifactCompressionNone && cfg.compression != appupdate.ArtifactCompressionGzip {
		return fmt.Errorf("artifact compression must be %s or %s", appupdate.ArtifactCompressionNone, appupdate.ArtifactCompressionGzip)
	}
	if cfg.notesFile != "" {
		data, err := os.ReadFile(cfg.notesFile)
		if err != nil {
			return fmt.Errorf("read release notes: %w", err)
		}
		cfg.notes = string(data)
	}
	publishedAt := time.Now().UTC()
	if cfg.publishedAt != "" {
		parsed, err := time.Parse(time.RFC3339, cfg.publishedAt)
		if err != nil {
			return fmt.Errorf("parse publication time: %w", err)
		}
		publishedAt = parsed
	}
	manifest := appupdate.Manifest{
		SchemaVersion: appupdate.ManifestSchemaVersion,
		Edition:       cfg.edition,
		Channel:       cfg.channel,
		Version:       cfg.version,
		Commit:        cfg.commit,
		PublishedAt:   publishedAt,
		Notes:         strings.TrimSpace(cfg.notes),
	}
	for _, value := range cfg.artifacts {
		target, path, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(target) == "" || strings.TrimSpace(path) == "" {
			return fmt.Errorf("parse artifact %q: want target=path", value)
		}
		artifact, err := inspectArtifact(strings.TrimSpace(target), strings.TrimSpace(path), cfg.compression)
		if err != nil {
			return err
		}
		manifest.Artifacts = append(manifest.Artifacts, artifact)
	}
	if err := appupdate.ValidateManifest(manifest); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode update manifest: %w", err)
	}
	data = append(data, '\n')
	privateKey, err := parsePrivateKey(cfg.privateKey)
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfg.output, data, 0o644); err != nil {
		return fmt.Errorf("write update manifest: %w", err)
	}
	signature := base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, data)) + "\n"
	if err := os.WriteFile(cfg.output+".sig", []byte(signature), 0o644); err != nil {
		return fmt.Errorf("write update signature: %w", err)
	}
	return nil
}

func inspectArtifact(target, path, compression string) (appupdate.Artifact, error) {
	file, err := os.Open(path)
	if err != nil {
		return appupdate.Artifact{}, fmt.Errorf("open artifact %s: %w", target, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return appupdate.Artifact{}, fmt.Errorf("inspect artifact %s: %w", target, err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return appupdate.Artifact{}, fmt.Errorf("hash artifact %s: %w", target, err)
	}
	artifact := appupdate.Artifact{
		Target:      target,
		Name:        filepath.Base(path),
		Compression: compression,
		Size:        info.Size(),
		SHA256:      hex.EncodeToString(hash.Sum(nil)),
	}
	switch compression {
	case appupdate.ArtifactCompressionNone:
		artifact.ExecutableSize = artifact.Size
		artifact.ExecutableSHA256 = artifact.SHA256
	case appupdate.ArtifactCompressionGzip:
		artifact.ExecutableSize, artifact.ExecutableSHA256, err = inspectGzipArtifact(path)
		if err != nil {
			return appupdate.Artifact{}, fmt.Errorf("inspect gzip artifact %s: %w", target, err)
		}
	default:
		return appupdate.Artifact{}, fmt.Errorf("inspect artifact %s: unsupported compression %q", target, compression)
	}
	return artifact, nil
}

func inspectGzipArtifact(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()

	buffered := bufio.NewReader(file)
	reader, err := gzip.NewReader(buffered)
	if err != nil {
		return 0, "", err
	}
	reader.Multistream(false)
	hash := sha256.New()
	size, copyErr := io.Copy(hash, reader)
	closeErr := reader.Close()
	if copyErr != nil {
		return 0, "", copyErr
	}
	if closeErr != nil {
		return 0, "", closeErr
	}
	_, err = buffered.ReadByte()
	if err == nil {
		return 0, "", errors.New("gzip artifact contains trailing data or multiple members")
	}
	if !errors.Is(err, io.EOF) {
		return 0, "", fmt.Errorf("read gzip trailer: %w", err)
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

func parsePrivateKey(value string) (ed25519.PrivateKey, error) {
	decoded, err := decodeBase64(strings.TrimSpace(value))
	if err != nil {
		return nil, errors.New("decode release private key: invalid base64")
	}
	if len(decoded) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(decoded), nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(decoded)
	if err != nil {
		return nil, errors.New("decode release private key: want raw Ed25519 or PKCS#8")
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("decode release private key: key is not Ed25519")
	}
	return privateKey, nil
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
