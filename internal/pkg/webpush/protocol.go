package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	recordSize      = 4096
	authSecretSize  = 16
	saltSize        = 16
	publicKeySize   = 65
	vapidExpiration = 12 * time.Hour
)

type urgency string

const (
	urgencyNormal urgency = "normal"
	urgencyHigh   urgency = "high"
)

type subscriptionKeys struct {
	publicKey *ecdh.PublicKey
	auth      []byte
}

type vapidPrivateKey struct {
	*ecdsa.PrivateKey
}

var (
	_ encoding.TextMarshaler   = vapidPrivateKey{}
	_ encoding.TextUnmarshaler = (*vapidPrivateKey)(nil)
)

func generateVAPIDKey() (*ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ECDSA key: %w", err)
	}
	return key, nil
}

func (k vapidPrivateKey) MarshalText() ([]byte, error) {
	if k.PrivateKey == nil {
		return nil, errors.New("VAPID private key is required")
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(k.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("encode VAPID private key: %w", err)
	}
	text := make([]byte, base64.RawURLEncoding.EncodedLen(len(encoded)))
	base64.RawURLEncoding.Encode(text, encoded)
	return text, nil
}

func (k *vapidPrivateKey) UnmarshalText(text []byte) error {
	encoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(text)))
	if err != nil {
		return fmt.Errorf("decode VAPID private key: %w", err)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(encoded)
	if err != nil {
		return fmt.Errorf("parse VAPID private key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return errors.New("VAPID private key must use ECDSA")
	}
	publicKey, err := key.PublicKey.ECDH()
	if err != nil {
		return fmt.Errorf("convert VAPID public key: %w", err)
	}
	if publicKey.Curve() != ecdh.P256() {
		return errors.New("VAPID private key must use P-256")
	}
	k.PrivateKey = key
	return nil
}

func vapidPublicKey(key *ecdsa.PrivateKey) (string, error) {
	if key == nil {
		return "", errors.New("VAPID private key is required")
	}
	publicKey, err := key.PublicKey.ECDH()
	if err != nil {
		return "", fmt.Errorf("convert VAPID public key: %w", err)
	}
	if publicKey.Curve() != ecdh.P256() {
		return "", errors.New("VAPID private key must use P-256")
	}
	return base64.RawURLEncoding.EncodeToString(publicKey.Bytes()), nil
}

func decodeSubscriptionKeys(auth, p256dh string) (subscriptionKeys, error) {
	authBytes, err := decodeSubscriptionKey(auth)
	if err != nil {
		return subscriptionKeys{}, fmt.Errorf("decode auth key: %w", err)
	}
	if len(authBytes) != authSecretSize {
		return subscriptionKeys{}, fmt.Errorf("auth key length is %d, want %d", len(authBytes), authSecretSize)
	}
	publicBytes, err := decodeSubscriptionKey(p256dh)
	if err != nil {
		return subscriptionKeys{}, fmt.Errorf("decode p256dh key: %w", err)
	}
	if len(publicBytes) != publicKeySize {
		return subscriptionKeys{}, fmt.Errorf("p256dh key length is %d, want %d", len(publicBytes), publicKeySize)
	}
	publicKey, err := ecdh.P256().NewPublicKey(publicBytes)
	if err != nil {
		return subscriptionKeys{}, fmt.Errorf("parse p256dh key: %w", err)
	}
	return subscriptionKeys{publicKey: publicKey, auth: authBytes}, nil
}

func decodeSubscriptionKey(value string) ([]byte, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "=")
	if strings.ContainsAny(value, "+/") {
		return base64.RawStdEncoding.DecodeString(value)
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func buildPushRequest(
	ctx context.Context,
	endpoint string,
	payload []byte,
	keys subscriptionKeys,
	vapidKey *ecdsa.PrivateKey,
	ttl int,
	priority urgency,
	now time.Time,
) (*http.Request, error) {
	body, err := encryptNotification(payload, keys, rand.Reader)
	if err != nil {
		return nil, err
	}
	authorization, err := vapidAuthorization(endpoint, defaultSubject, vapidKey, now.Add(vapidExpiration), rand.Reader)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create push request: %w", err)
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(ttl))
	req.Header.Set("Urgency", string(priority))
	return req, nil
}

func encryptNotification(payload []byte, keys subscriptionKeys, random io.Reader) ([]byte, error) {
	localPrivateKey, err := ecdh.P256().GenerateKey(random)
	if err != nil {
		return nil, fmt.Errorf("generate content encryption key: %w", err)
	}
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(random, salt); err != nil {
		return nil, fmt.Errorf("generate content encryption salt: %w", err)
	}
	return encryptNotificationWithKey(payload, keys, localPrivateKey, salt)
}

func encryptNotificationWithKey(
	payload []byte,
	keys subscriptionKeys,
	localPrivateKey *ecdh.PrivateKey,
	salt []byte,
) ([]byte, error) {
	if len(payload)+17 > recordSize {
		return nil, fmt.Errorf("push payload is %d bytes, maximum is %d", len(payload), recordSize-17)
	}
	if localPrivateKey == nil {
		return nil, errors.New("content encryption private key is required")
	}
	if len(salt) != saltSize {
		return nil, fmt.Errorf("content encryption salt length is %d, want %d", len(salt), saltSize)
	}
	localPublicKey := localPrivateKey.PublicKey().Bytes()
	sharedSecret, err := localPrivateKey.ECDH(keys.publicKey)
	if err != nil {
		return nil, fmt.Errorf("derive shared content secret: %w", err)
	}
	keyInfo := append([]byte("WebPush: info\x00"), keys.publicKey.Bytes()...)
	keyInfo = append(keyInfo, localPublicKey...)
	ikm, err := hkdf.Key(sha256.New, sharedSecret, keys.auth, string(keyInfo), 32)
	if err != nil {
		return nil, fmt.Errorf("derive content IKM: %w", err)
	}
	contentKey, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, fmt.Errorf("derive content encryption key: %w", err)
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, fmt.Errorf("derive content nonce: %w", err)
	}
	block, err := aes.NewCipher(contentKey)
	if err != nil {
		return nil, fmt.Errorf("create content cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create content GCM: %w", err)
	}
	plaintext := append(bytes.Clone(payload), 2)
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	header := make([]byte, saltSize+4+1+len(localPublicKey))
	copy(header, salt)
	binary.BigEndian.PutUint32(header[saltSize:saltSize+4], recordSize)
	header[saltSize+4] = byte(len(localPublicKey))
	copy(header[saltSize+5:], localPublicKey)
	return append(header, ciphertext...), nil
}

func vapidAuthorization(
	endpoint string,
	subject string,
	key *ecdsa.PrivateKey,
	expiresAt time.Time,
	random io.Reader,
) (string, error) {
	if key == nil {
		return "", errors.New("VAPID private key is required")
	}
	endpointURL, err := url.Parse(endpoint)
	if err != nil || endpointURL.Scheme != "https" || endpointURL.Host == "" {
		return "", errors.New("push endpoint must be an HTTPS URL")
	}
	audience := (&url.URL{Scheme: endpointURL.Scheme, Host: endpointURL.Host}).String()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(struct {
		Audience string `json:"aud"`
		Expires  int64  `json:"exp"`
		Subject  string `json:"sub"`
	}{Audience: audience, Expires: expiresAt.Unix(), Subject: subject})
	if err != nil {
		return "", fmt.Errorf("encode VAPID claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(claims)
	unsigned := header + "." + payload
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(random, key, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign VAPID token: %w", err)
	}
	signature, err := encodeECDSASignature(r, s)
	if err != nil {
		return "", err
	}
	publicKey, err := vapidPublicKey(key)
	if err != nil {
		return "", err
	}
	token := unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	return "vapid t=" + token + ", k=" + publicKey, nil
}

func encodeECDSASignature(r, s *big.Int) ([]byte, error) {
	if r == nil || s == nil || r.Sign() <= 0 || s.Sign() <= 0 || r.BitLen() > 256 || s.BitLen() > 256 {
		return nil, errors.New("invalid ECDSA signature")
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return signature, nil
}
