// Package nativepush sends encrypted notifications through Sigmo's public APNs gateway.
package nativepush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

// Envelope authenticates routing identity while keeping message data opaque to the gateway.
type Envelope struct {
	Version        int    `json:"version"`
	SubscriptionID string `json:"subscriptionId"`
	EventID        string `json:"eventId"`
	Nonce          string `json:"nonce"`
	Ciphertext     string `json:"ciphertext"`
}

// Encryption describes one installation-specific notification.
type Encryption struct {
	Key            []byte
	SubscriptionID string
	EventID        string
	Content        storage.PushContent
}

// Encrypt uses AES-256-GCM, a 12-byte random nonce and a 16-byte appended tag.
func Encrypt(input Encryption) (Envelope, error) {
	return encryptNonce(input, nil)
}

func encryptNonce(input Encryption, nonce []byte) (Envelope, error) {
	result := Envelope{Version: 1, SubscriptionID: input.SubscriptionID, EventID: input.EventID}
	if len(input.Key) != 32 {
		return result, errors.New("push encryption requires a 256-bit key")
	}
	block, err := aes.NewCipher(input.Key)
	if err != nil {
		return result, fmt.Errorf("create push cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return result, fmt.Errorf("create push authentication: %w", err)
	}
	if nonce == nil {
		nonce = make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return result, fmt.Errorf("generate push nonce: %w", err)
		}
	}
	if len(nonce) != gcm.NonceSize() {
		return result, errors.New("invalid push nonce size")
	}
	// The encrypted preview, base64 overhead and APNs metadata must fit in 4096 bytes.
	input.Content.BackendName = truncate(input.Content.BackendName, 96)
	input.Content.ModemName = truncate(input.Content.ModemName, 96)
	input.Content.ProfileName = truncate(input.Content.ProfileName, 96)
	input.Content.Sender = truncate(input.Content.Sender, 128)
	input.Content.Text = truncate(input.Content.Text, 1200)
	plain, err := json.Marshal(input.Content)
	if err != nil {
		return result, fmt.Errorf("encode push preview: %w", err)
	}
	if len(plain) > 1900 {
		input.Content.Text = truncate(input.Content.Text, max(0, len(input.Content.Text)-(len(plain)-1900)-3))
		plain, err = json.Marshal(input.Content)
		if err != nil {
			return result, fmt.Errorf("encode shortened push preview: %w", err)
		}
	}
	if len(plain) > 1900 {
		return result, errors.New("push metadata exceeds payload budget")
	}
	aad := []byte("sigmo-push:v1:" + input.SubscriptionID + ":" + input.EventID)
	result.Nonce = base64.StdEncoding.EncodeToString(nonce)
	result.Ciphertext = base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, plain, aad))
	return result, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	if limit < 3 {
		return ""
	}
	end := limit - 3
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + "…"
}
