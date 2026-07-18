package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"net/url"
	"strings"
	"testing"
	"time"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

func TestVAPIDPrivateKeyTextRoundTrip(t *testing.T) {
	key, err := generateVAPIDKey()
	if err != nil {
		t.Fatalf("generateVAPIDKey() error = %v", err)
	}
	text, err := (vapidPrivateKey{PrivateKey: key}).MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}

	var decoded vapidPrivateKey
	if err := decoded.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}
	if decoded.D.Cmp(key.D) != 0 || !decoded.PublicKey.Equal(&key.PublicKey) {
		t.Fatal("UnmarshalText() key does not match original")
	}
}

func TestVAPIDPrivateKeyUnmarshalTextRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "invalid base64url", value: "%"},
		{name: "invalid PKCS8", value: base64.RawURLEncoding.EncodeToString([]byte("not a key"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var key vapidPrivateKey
			if err := key.UnmarshalText([]byte(tt.value)); err == nil {
				t.Fatal("UnmarshalText() error = nil, want error")
			}
		})
	}
}

func TestVAPIDAuthorizationIsVerifiable(t *testing.T) {
	key, err := generateVAPIDKey()
	if err != nil {
		t.Fatalf("generateVAPIDKey() error = %v", err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	authorization, err := vapidAuthorization(
		"https://push.example.test/send/abc",
		defaultSubject,
		key,
		now.Add(time.Hour),
		bytes.NewReader(bytes.Repeat([]byte{0x42}, 256)),
	)
	if err != nil {
		t.Fatalf("vapidAuthorization() error = %v", err)
	}
	parts := strings.Split(strings.TrimPrefix(authorization, "vapid "), ", ")
	if len(parts) != 2 {
		t.Fatalf("authorization parts = %d, want 2", len(parts))
	}
	token := strings.TrimPrefix(parts[0], "t=")
	jwtParts := strings.Split(token, ".")
	if len(jwtParts) != 3 {
		t.Fatalf("JWT parts = %d, want 3", len(jwtParts))
	}
	var claims struct {
		Audience string `json:"aud"`
		Expires  int64  `json:"exp"`
		Subject  string `json:"sub"`
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(jwtParts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		t.Fatalf("decode claims JSON: %v", err)
	}
	parsedEndpoint, _ := url.Parse("https://push.example.test/send/abc")
	if claims.Audience != parsedEndpoint.Scheme+"://"+parsedEndpoint.Host {
		t.Fatalf("audience = %q, want endpoint origin", claims.Audience)
	}
	if claims.Expires != now.Add(time.Hour).Unix() || claims.Subject != defaultSubject {
		t.Fatalf("claims = %+v", claims)
	}
	signature, err := base64.RawURLEncoding.DecodeString(jwtParts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if len(signature) != 64 {
		t.Fatalf("signature length = %d, want 64", len(signature))
	}
	digest := sha256.Sum256([]byte(jwtParts[0] + "." + jwtParts[1]))
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(&key.PublicKey, digest[:], r, s) {
		t.Fatal("VAPID signature did not verify")
	}
}

func TestEncryptNotificationRoundTrip(t *testing.T) {
	clientPrivate, err := ecdh.P256().NewPrivateKey(bytes.Repeat([]byte{0x78}, 32))
	if err != nil {
		t.Fatalf("NewPrivateKey() client error = %v", err)
	}
	auth := bytes.Repeat([]byte{0x37}, authSecretSize)
	keys := subscriptionKeys{publicKey: clientPrivate.PublicKey(), auth: auth}
	payload := []byte(`{"type":"sms","text":"hello"}`)
	serverPrivate, err := ecdh.P256().NewPrivateKey(bytes.Repeat([]byte{0x19}, 32))
	if err != nil {
		t.Fatalf("NewPrivateKey() server error = %v", err)
	}
	body, err := encryptNotificationWithKey(
		payload,
		keys,
		serverPrivate,
		bytes.Repeat([]byte{0x19}, saltSize),
	)
	if err != nil {
		t.Fatalf("encryptNotificationWithKey() error = %v", err)
	}
	const wantBody = "GRkZGRkZGRkZGRkZGRkZGQAAEABBBBJQvKVLtoOfEjYb2WFeC9DDsIKkRx0SQa6BgLxVEVbB9QJrVlSWautDHX4GmpGMxJu4Wn2PDiUkqbTYS5RS9wQPu2jbMRjlSUYV_Qbk31mmjUI4wTwrjqG1OqFO_rAQox9TCQj6PD8x-KzHx2TA"
	if got := base64.RawURLEncoding.EncodeToString(body); got != wantBody {
		t.Fatalf("encrypted body = %q, want regression vector", got)
	}
	if len(body) <= saltSize+4+1+publicKeySize {
		t.Fatalf("encrypted body is too short: %d", len(body))
	}
	salt := body[:saltSize]
	recordSizeValue := binary.BigEndian.Uint32(body[saltSize : saltSize+4])
	if recordSizeValue != recordSize {
		t.Fatalf("record size = %d, want %d", recordSizeValue, recordSize)
	}
	keyLength := int(body[saltSize+4])
	if keyLength != publicKeySize {
		t.Fatalf("ephemeral key length = %d, want %d", keyLength, publicKeySize)
	}
	ephemeral, err := ecdh.P256().NewPublicKey(body[saltSize+5 : saltSize+5+keyLength])
	if err != nil {
		t.Fatalf("parse ephemeral key: %v", err)
	}
	sharedSecret, err := clientPrivate.ECDH(ephemeral)
	if err != nil {
		t.Fatalf("derive shared secret: %v", err)
	}
	keyInfo := append([]byte("WebPush: info\x00"), clientPrivate.PublicKey().Bytes()...)
	keyInfo = append(keyInfo, ephemeral.Bytes()...)
	ikm, err := hkdf.Key(sha256.New, sharedSecret, auth, string(keyInfo), 32)
	if err != nil {
		t.Fatalf("derive IKM: %v", err)
	}
	contentKey, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		t.Fatalf("derive content key: %v", err)
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		t.Fatalf("derive nonce: %v", err)
	}
	block, err := aes.NewCipher(contentKey)
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("NewGCM() error = %v", err)
	}
	plaintext, err := gcm.Open(nil, nonce, body[saltSize+5+keyLength:], nil)
	if err != nil {
		t.Fatalf("decrypt body: %v", err)
	}
	if !bytes.Equal(plaintext, append(payload, 2)) {
		t.Fatalf("plaintext = %q, want %q", plaintext, append(payload, 2))
	}
}

func TestPayloadForEvent(t *testing.T) {
	tests := []struct {
		name  string
		event notifyevent.Event
		want  bool
		tag   string
	}{
		{
			name:  "incoming sms",
			event: notifyevent.SMSEvent{ID: "sms-1", ModemID: "modem-1", Incoming: true, From: "10086"},
			want:  true,
			tag:   "sms:sms-1",
		},
		{
			name:  "outgoing sms",
			event: notifyevent.SMSEvent{ID: "sms-1", ModemID: "modem-1"},
		},
		{
			name:  "ringing call",
			event: notifyevent.CallEvent{ID: "call-1", ModemID: "modem-1", Incoming: true, State: "ringing"},
			want:  true,
			tag:   "call:call-1",
		},
		{
			name:  "active call",
			event: notifyevent.CallEvent{ID: "call-1", ModemID: "modem-1", Incoming: true, State: "active"},
		},
		{
			name: "reminder",
			event: notifyevent.ReminderEvent{
				ProfileType: "esim",
				ProfileID:   "iccid",
				ModemID:     "modem-1",
				ScheduledAt: time.Unix(1_700_000_000, 0),
			},
			want: true,
			tag:  "reminder:esim:iccid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, _, _, ok := payloadForEvent(tt.event)
			if ok != tt.want {
				t.Fatalf("payloadForEvent() ok = %v, want %v", ok, tt.want)
			}
			if tt.want && payload.Tag != tt.tag {
				t.Fatalf("payload tag = %q, want %q", payload.Tag, tt.tag)
			}
		})
	}
}
