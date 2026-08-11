package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
)

const identitySchemaVersion = 1

var hostIdentityPaths = []string{
	"/etc/machine-id",
	"/sys/class/dmi/id/product_uuid",
}

type identity struct {
	DeviceID        string
	PublicKey       ed25519.PublicKey
	PrivateKey      ed25519.PrivateKey
	FingerprintHash string
}

type storedIdentity struct {
	SchemaVersion int    `json:"schemaVersion"`
	PrivateKey    string `json:"privateKey"`
	HostSecret    string `json:"hostSecret"`
}

func loadOrCreateIdentity(path string) (identity, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return createIdentity(path)
	}
	if err != nil {
		return identity{}, fmt.Errorf("read device identity: %w", err)
	}
	var stored storedIdentity
	if err := json.Unmarshal(data, &stored); err != nil {
		return identity{}, fmt.Errorf("decode device identity: %w", err)
	}
	if stored.SchemaVersion != identitySchemaVersion {
		return identity{}, errors.New("decode device identity: unsupported schema version")
	}
	privateKey, err := base64.RawStdEncoding.DecodeString(stored.PrivateKey)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return identity{}, errors.New("decode device identity: invalid Ed25519 private key")
	}
	hostSecret, err := base64.RawStdEncoding.DecodeString(stored.HostSecret)
	if err != nil || len(hostSecret) != 32 {
		return identity{}, errors.New("decode device identity: invalid host secret")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return identity{}, fmt.Errorf("protect device identity: %w", err)
	}
	key := ed25519.PrivateKey(privateKey)
	return newIdentity(key.Public().(ed25519.PublicKey), key, hostSecret), nil
}

func createIdentity(path string) (identity, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return identity{}, fmt.Errorf("generate device key: %w", err)
	}
	hostSecret := make([]byte, 32)
	if _, err := rand.Read(hostSecret); err != nil {
		return identity{}, fmt.Errorf("generate host secret: %w", err)
	}
	stored := storedIdentity{
		SchemaVersion: identitySchemaVersion,
		PrivateKey:    base64.RawStdEncoding.EncodeToString(privateKey),
		HostSecret:    base64.RawStdEncoding.EncodeToString(hostSecret),
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return identity{}, fmt.Errorf("encode device identity: %w", err)
	}
	data = append(data, '\n')
	if err := writeIdentity(path, data); err != nil {
		return identity{}, err
	}
	return newIdentity(publicKey, privateKey, hostSecret), nil
}

func writeIdentity(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create identity directory: %w", err)
	}
	file, err := os.CreateTemp(dir, ".device.identity-*")
	if err != nil {
		return fmt.Errorf("create device identity: %w", err)
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err := file.Chmod(0o600); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close device identity: %w", closeErr))
		}
		return fmt.Errorf("protect device identity: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close device identity: %w", closeErr))
		}
		return fmt.Errorf("write device identity: %w", err)
	}
	if err := file.Sync(); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close device identity: %w", closeErr))
		}
		return fmt.Errorf("sync device identity: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close device identity: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("install device identity: %w", err)
	}
	if err := syncIdentityDirectory(dir); err != nil {
		return err
	}
	return nil
}

func syncIdentityDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open device identity directory: %w", err)
	}
	if err := dir.Sync(); err != nil {
		if closeErr := dir.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close device identity directory: %w", closeErr))
		}
		return fmt.Errorf("sync device identity directory: %w", err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("close device identity directory: %w", err)
	}
	return nil
}

func newIdentity(publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey, hostSecret []byte) identity {
	deviceSum := sha256.Sum256(publicKey)
	fingerprint := sha256.New()
	writeFingerprintComponent(fingerprint, []byte("sigmo-host-v1"))
	writeFingerprintComponent(fingerprint, hostSecret)
	for _, path := range hostIdentityPaths {
		value, err := os.ReadFile(path)
		if err != nil {
			value = nil
		}
		writeFingerprintComponent(fingerprint, []byte(strings.TrimSpace(string(value))))
	}
	return identity{
		DeviceID:        hex.EncodeToString(deviceSum[:16]),
		PublicKey:       publicKey,
		PrivateKey:      privateKey,
		FingerprintHash: base64.RawURLEncoding.EncodeToString(fingerprint.Sum(nil)),
	}
}

func writeFingerprintComponent(dst hash.Hash, value []byte) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = dst.Write(size[:])
	_, _ = dst.Write(value)
}
