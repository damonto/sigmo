package license

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	licenseStateScope = "license"
	identityStateKey  = "device.identity"
)

type identity struct {
	DeviceID   string
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

type storedIdentity struct {
	PrivateKey string `json:"privateKey"`
}

func loadOrCreateIdentity(ctx context.Context, db *storage.Store) (identity, error) {
	var stored storedIdentity
	err := db.Get(ctx, licenseStateScope, identityStateKey, &stored)
	if errors.Is(err, storage.ErrNotFound) {
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return identity{}, fmt.Errorf("generate device key: %w", err)
		}
		stored.PrivateKey = base64.RawStdEncoding.EncodeToString(privateKey)
		if err := db.Put(ctx, licenseStateScope, identityStateKey, stored); err != nil {
			return identity{}, fmt.Errorf("save device key: %w", err)
		}
		return newIdentity(publicKey, privateKey), nil
	}
	if err != nil {
		return identity{}, fmt.Errorf("read device key: %w", err)
	}
	privateKey, err := base64.RawStdEncoding.DecodeString(stored.PrivateKey)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return identity{}, errors.New("decode device key: invalid Ed25519 private key")
	}
	key := ed25519.PrivateKey(privateKey)
	publicKey := key.Public().(ed25519.PublicKey)
	return newIdentity(publicKey, key), nil
}

func newIdentity(publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey) identity {
	sum := sha256.Sum256(publicKey)
	return identity{
		DeviceID:   hex.EncodeToString(sum[:16]),
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}
}
