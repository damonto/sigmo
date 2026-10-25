package license

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/damonto/sigmo/internal/pkg/carrierconfig"
	jose "github.com/go-jose/go-jose/v4"
)

func TestWorkerConfigurationInteroperability(t *testing.T) {
	data, err := os.ReadFile("testdata/configuration.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Configuration, PrivateKey, LicensePublicKey string }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	keyDER, err := base64.StdEncoding.DecodeString(fixture.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := jose.ParseEncrypted(fixture.Configuration, []jose.KeyAlgorithm{jose.ECDH_ES}, []jose.ContentEncryption{jose.A256GCM})
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := encrypted.Decrypt(key)
	if err != nil {
		t.Fatal(err)
	}
	var signed struct{ Payload, Signature string }
	if err := json.Unmarshal(plaintext, &signed); err != nil {
		t.Fatal(err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	signature, _ := base64.StdEncoding.DecodeString(signed.Signature)
	publicKey, _ := base64.StdEncoding.DecodeString(fixture.LicensePublicKey)
	if !ed25519.Verify(publicKey, append([]byte(carrierconfig.SignatureContext), payload...), signature) {
		t.Fatal("Worker signature was not verified")
	}
	var grant configurationGrant
	if err := json.Unmarshal(payload, &grant); err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.RawURLEncoding.DecodeString(grant.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := carrierconfig.Unpack(compressed, grant.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationCacheLifecycle(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := newTestController(t, Config{Storage: openTestStorage(t), LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey), BaseURL: "https://license.example"})
	if err != nil {
		t.Fatal(err)
	}
	session := validSession(t)
	installSession(t, controller, session)
	lease := validLease(controller.identity.DeviceID, "Test")
	lease.SessionID = session.SessionID
	lease.Generation = session.Generation
	proof := signedProof(t, privateKey, lease)
	controller.setLease(&lease, &proof)
	calls := 0
	controller.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return configurationResponse(t, req, controller, privateKey), nil
	})}
	first, err := controller.CarrierConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := controller.CarrierConfig(t.Context())
	if err != nil || first != second || calls != 1 {
		t.Fatalf("cache refresh calls=%d, err=%v", calls, err)
	}
	cache, err := os.ReadFile(controller.configuration.path)
	if err != nil || !json.Valid([]byte(`"`+string(cache)+`"`)) {
		t.Fatalf("encrypted cache read: %v", err)
	}
	if info, err := os.Stat(controller.configuration.path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache permissions: %v, %v", info, err)
	}
	restarted, err := newTestController(t, Config{
		Storage: controller.storage, IdentityPath: strings.TrimSuffix(controller.configuration.path, ".configs"),
		LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey), BaseURL: "https://license.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.Authorized() {
		t.Fatal("verified cache did not restore authorization")
	}
	if _, err := restarted.CarrierConfig(t.Context()); err != nil {
		t.Fatalf("cache after restart: %v", err)
	}
	restarted.configuration.grant.RefreshAfter = time.Now().Add(-time.Second)
	if _, err := restarted.CarrierConfig(t.Context()); err != nil {
		t.Fatalf("offline cache after refresh deadline: %v", err)
	}
	restarted.revokeConfiguration(restarted.currentSession())
	if restarted.Authorized() {
		t.Fatal("revoked cache still grants access")
	}
	// The second controller removed the shared disk cache; the original process
	// still has its own verified copy until its deadline or explicit revocation.
	controller.configuration.grant.RefreshAfter = time.Now().Add(-time.Second)
	controller.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	if cached, err := controller.CarrierConfig(t.Context()); err != nil || cached != first {
		t.Fatalf("offline cache: %v", err)
	}
	controller.configuration.grant.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := controller.CarrierConfig(t.Context()); err == nil {
		t.Fatal("used expired cache")
	}
}

func mustDecode(t *testing.T, value string) []byte {
	t.Helper()
	data, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestConfigurationKeyIsIndependentAndPersistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "configuration.key")
	first, err := loadConfigurationKey(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadConfigurationKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equal(second) {
		t.Fatal("configuration key changed on restart")
	}
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if first.Equal(other) {
		t.Fatal("configuration keys are shared")
	}
}

func configurationResponse(t *testing.T, req *http.Request, controller *Controller, privateKey ed25519.PrivateKey) *http.Response {
	t.Helper()
	compressed, revision, err := carrierconfig.Pack(carrierconfig.Files{SchemaVersion: 1, Entitlement: json.RawMessage(`[]`), WFC: json.RawMessage(`[]`), IMS: json.RawMessage(`[]`)})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := base64.RawURLEncoding.DecodeString(req.URL.Query().Get("recipient"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.ParsePKIXPublicKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(controller.identity.PublicKey, []byte("GET\n"+req.URL.RequestURI()+"\n"+req.Header.Get("X-Sigmo-Timestamp")), mustDecode(t, req.Header.Get("X-Sigmo-Signature"))) {
		t.Fatal("recipient was not covered by the device signature")
	}
	grant := configurationGrant{SchemaVersion: 1, DeviceID: controller.identity.DeviceID, SessionID: controller.currentSession().SessionID, Revision: revision, IssuedAt: time.Now(), RefreshAfter: time.Now().Add(time.Minute), ExpiresAt: time.Now().Add(time.Hour), Bundle: base64.RawURLEncoding.EncodeToString(compressed)}
	payload, _ := json.Marshal(grant)
	signed, _ := json.Marshal(map[string]string{"payload": base64.RawURLEncoding.EncodeToString(payload), "signature": base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, append([]byte(carrierconfig.SignatureContext), payload...)))})
	encrypter, err := jose.NewEncrypter(jose.A256GCM, jose.Recipient{Algorithm: jose.ECDH_ES, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := encrypter.Encrypt(signed)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := encrypted.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return response(http.StatusOK, map[string]string{"configuration": compact})
}

func authorizedConfigurationController(t *testing.T) (*Controller, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := newTestController(t, Config{Storage: openTestStorage(t), BaseURL: "https://license.example", LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey)})
	if err != nil {
		t.Fatal(err)
	}
	session := validSession(t)
	installSession(t, controller, session)
	lease := validLease(controller.identity.DeviceID, "Test")
	lease.SessionID, lease.Generation = session.SessionID, session.Generation
	proof := signedProof(t, privateKey, lease)
	controller.setLease(&lease, &proof)
	return controller, privateKey
}

func TestRevocationDoesNotWaitForConfigurationDownload(t *testing.T) {
	controller, privateKey := authorizedConfigurationController(t)
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	controller.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reply := configurationResponse(t, req, controller, privateKey)
		close(started)
		select {
		case <-release:
			return reply, nil
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	})}
	result := make(chan error, 1)
	go func() { _, err := controller.CarrierConfig(t.Context()); result <- err }()
	select {
	case <-started:
	case err := <-result:
		t.Fatalf("configuration request stopped before download: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("configuration request did not start")
	}
	revoked := make(chan struct{})
	go func() { controller.revokeConfiguration(controller.currentSession()); close(revoked) }()
	select {
	case <-revoked:
	case <-time.After(5 * time.Second):
		t.Fatal("revocation blocked on the configuration HTTP request")
	}
	if controller.Authorized() {
		t.Fatal("revoked authorization remains active")
	}
	unblock()
	if err := <-result; !errors.Is(err, errExplicitUnauthorized) {
		t.Fatalf("revoked download = %v", err)
	}
	if controller.Authorized() {
		t.Fatal("late configuration response restored authorization")
	}
	if _, err := os.Stat(controller.configuration.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("late response recreated revoked cache: %v", err)
	}
}

func TestCurrentLeaseOverridesOlderConfigurationGrant(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		controller, privateKey := authorizedConfigurationController(t)
		controller.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return configurationResponse(t, req, controller, privateKey), nil
		})}
		if _, err := controller.CarrierConfig(t.Context()); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		lease := validLease(controller.identity.DeviceID, "Shortened entitlement")
		lease.IssuedAt, lease.RefreshAfter, lease.ExpiresAt = now, now.Add(time.Minute), now.Add(2*time.Minute)
		proof := signedProof(t, privateKey, lease)
		controller.setLease(&lease, &proof)
		time.Sleep(3 * time.Minute)
		if controller.Authorized() {
			t.Fatal("older configuration extended a newer lease")
		}
		if _, err := controller.CarrierConfig(context.Background()); !errors.Is(err, errExplicitUnauthorized) {
			t.Fatalf("expired authorization returned configuration: %v", err)
		}
	})
}

func TestOldConfigurationRequestCannotRevokeNewPairing(t *testing.T) {
	controller, privateKey := authorizedConfigurationController(t)
	next := validSession(t)
	next.SessionID = "new-pairing-session"
	// Pairing persists the new session before publishing its lease. The outgoing
	// request must retain the identity of the old proof it actually signs.
	installSession(t, controller, next)
	controller.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		proofData, err := base64.RawURLEncoding.DecodeString(req.Header.Get("X-Sigmo-Lease"))
		if err != nil {
			t.Fatal(err)
		}
		var proof signedLease
		if err := json.Unmarshal(proofData, &proof); err != nil {
			t.Fatal(err)
		}
		var sent Lease
		if err := json.Unmarshal(proof.Lease, &sent); err != nil {
			t.Fatal(err)
		}
		if sent.SessionID == next.SessionID {
			t.Fatal("expected the in-flight request to carry the old lease")
		}
		lease := validLease(controller.identity.DeviceID, "New pairing")
		lease.SessionID, lease.Generation = next.SessionID, next.Generation
		nextProof := signedProof(t, privateKey, lease)
		controller.setLease(&lease, &nextProof)
		return response(http.StatusConflict, errorResponse{ErrorCode: "license_session_superseded", Message: "old session superseded"}), nil
	})}
	if _, err := controller.CarrierConfig(t.Context()); err == nil {
		t.Fatal("superseded request succeeded")
	}
	if !controller.Authorized() || controller.configuration.denied {
		t.Fatal("old request revoked the newly paired session")
	}
	if licensee := controller.Licensee(); licensee == nil || licensee.DisplayName != "New pairing" {
		t.Fatalf("new pairing was lost: %+v", licensee)
	}
}
