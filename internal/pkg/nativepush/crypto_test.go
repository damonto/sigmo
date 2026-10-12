package nativepush

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestEncryptionCrossLanguageVector(t *testing.T) {
	data, err := os.ReadFile("../../../apps/ios/SigmoTests/Fixtures/native_push.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Key      []byte              `json:"key"`
		Envelope Envelope            `json:"envelope"`
		Content  storage.PushContent `json:"content"`
	}
	if err := json.Unmarshal(data, &vector); err != nil {
		t.Fatal(err)
	}
	nonce, err := base64.StdEncoding.DecodeString(vector.Envelope.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	input := Encryption{Key: vector.Key, SubscriptionID: vector.Envelope.SubscriptionID, EventID: vector.Envelope.EventID, Content: vector.Content}
	got, err := encryptNonce(input, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if got != vector.Envelope {
		t.Fatal("Go encryption differs from the Web Crypto/Swift fixture")
	}
	for _, name := range []string{"event", "subscription", "ciphertext", "nonce"} {
		t.Run(name, func(t *testing.T) {
			changed := got
			switch name {
			case "event":
				changed.EventID += "1"
			case "subscription":
				changed.SubscriptionID += "1"
			case "ciphertext":
				changed.Ciphertext = "AAAA" + changed.Ciphertext[4:]
			case "nonce":
				changed.Nonce = "AAAA" + changed.Nonce[4:]
			}
			if _, err := decryptPreview(vector.Key, changed); err == nil {
				t.Fatal("tampered envelope was authenticated")
			}
		})
	}
}

func TestPreviewBudgetAndUTF8(t *testing.T) {
	for _, text := range []string{strings.Repeat("短信👋", 1000), strings.Repeat("<\n\\\"", 1000), strings.Repeat("a", 10000)} {
		t.Run(text[:1], func(t *testing.T) {
			input := Encryption{Key: make([]byte, 32), SubscriptionID: "subscription", EventID: "sms:1", Content: storage.PushContent{Kind: "sms", BackendID: "backend", BackendName: strings.Repeat("长名字", 80), ConversationID: "conversation", Sender: "10086", Text: text}}
			value, err := Encrypt(input)
			if err != nil {
				t.Fatal(err)
			}
			plain, err := decryptPreview(input.Key, value)
			if err != nil {
				t.Fatal(err)
			}
			if len(plain) > 1900 || !utf8.Valid(plain) {
				t.Fatalf("invalid preview budget/UTF-8: %d", len(plain))
			}
			var content storage.PushContent
			if err := json.Unmarshal(plain, &content); err != nil {
				t.Fatal(err)
			}
			if content.ConversationID != "conversation" {
				t.Fatal("truncation removed navigation")
			}
		})
	}
	if _, err := Encrypt(Encryption{Key: make([]byte, 16)}); err == nil {
		t.Fatal("accepted a non-256-bit key")
	}
}

func decryptPreview(key []byte, envelope Envelope) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return nil, err
	}
	sealed, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, sealed, []byte("sigmo-push:v1:"+envelope.SubscriptionID+":"+envelope.EventID))
}
