package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appupdate "github.com/damonto/sigmo/internal/app/update"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestIdentityPersistsInStorage(t *testing.T) {
	db := openTestStorage(t)
	first, err := New(t.Context(), Config{Storage: db})
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(t.Context(), Config{Storage: db})
	if err != nil {
		t.Fatal(err)
	}
	if first.identity.DeviceID != second.identity.DeviceID {
		t.Fatalf("device IDs differ: %q and %q", first.identity.DeviceID, second.identity.DeviceID)
	}
	var stored storedIdentity
	if err := db.Get(t.Context(), licenseStateScope, identityStateKey, &stored); err != nil {
		t.Fatalf("read stored identity: %v", err)
	}
	if stored.PrivateKey == "" {
		t.Fatal("stored private key is empty")
	}
}

func TestLicenseeReturnsDefensiveTimeCopies(t *testing.T) {
	controller, err := New(t.Context(), Config{Storage: openTestStorage(t)})
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	lease := validLease(controller.identity.DeviceID, "Test User")
	lease.EntitlementExpiresAt = &expiresAt
	controller.setLease(&lease, nil)

	licensee := controller.Licensee()
	if licensee == nil || licensee.ExpiresAt == nil {
		t.Fatalf("Licensee() = %+v", licensee)
	}
	*licensee.ExpiresAt = time.Time{}
	if got := controller.Licensee(); got == nil || got.ExpiresAt == nil || got.ExpiresAt.IsZero() {
		t.Fatalf("Licensee() leaked internal time pointer: %+v", got)
	}
}

func TestNewIgnoresRuntimeAuthorizationEnvironment(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIGMO_PRO_WORKER_URL", "https://attacker.example")
	t.Setenv("SIGMO_PRO_LICENSE_PUBLIC_KEY", base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize)))

	controller, err := New(t.Context(), Config{
		BaseURL:          "https://license.example",
		LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
		Storage:          openTestStorage(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if controller.baseURL != "https://license.example" {
		t.Fatalf("base URL = %q", controller.baseURL)
	}
	if !controller.licensePublicKey.Equal(publicKey) {
		t.Fatal("license public key was replaced by runtime environment")
	}
}

func TestNewValidatesAuthorizationServiceURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{name: "https", baseURL: "https://license.example/v1"},
		{name: "loopback http", baseURL: "http://127.0.0.1:8787"},
		{name: "external http", baseURL: "http://license.example", wantErr: true},
		{name: "relative", baseURL: "/license", wantErr: true},
		{name: "credentials", baseURL: "https://user:pass@license.example", wantErr: true},
		{name: "query", baseURL: "https://license.example?tenant=one", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(t.Context(), Config{BaseURL: tt.baseURL, Storage: openTestStorage(t)})
			if (err != nil) != tt.wantErr {
				t.Fatalf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAuthorizationClientDoesNotFollowRedirects(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests > 1 {
			t.Fatalf("authorization client followed redirect to %s", req.URL)
		}
		resp := response(http.StatusFound, map[string]string{})
		resp.Header.Set("Location", "https://attacker.example/collect")
		resp.Request = req
		return resp, nil
	})}
	controller, err := New(t.Context(), Config{
		BaseURL: "https://license.example",
		Storage: openTestStorage(t),
		Client:  client,
	})
	if err != nil {
		t.Fatal(err)
	}

	status, err := controller.doJSON(t.Context(), http.MethodPost, "/v1/license-challenges", map[string]string{"deviceId": controller.identity.DeviceID}, "", nil)
	if status != http.StatusFound || err == nil {
		t.Fatalf("doJSON() status = %d, error = %v", status, err)
	}
	if requests != 1 {
		t.Fatalf("HTTP requests = %d, want 1", requests)
	}
	if client.CheckRedirect != nil {
		t.Fatal("New() mutated the caller's HTTP client")
	}
}

func TestSameOriginNormalizesDefaultPorts(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "implicit HTTPS port", a: "https://license.example", b: "https://license.example:443", want: true},
		{name: "different port", a: "https://license.example", b: "https://license.example:8443"},
		{name: "different scheme", a: "https://license.example", b: "http://license.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := url.Parse(tt.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := url.Parse(tt.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := sameOrigin(a, b); got != tt.want {
				t.Fatalf("sameOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateCreatedPairing(t *testing.T) {
	now := time.Now()
	valid := pairing{
		ID:            "pairing-id",
		PollToken:     "poll-token",
		ActivationURL: "https://t.me/SigmoProBot?start=pairing-id",
		Status:        "pending",
		ExpiresAt:     now.Add(time.Minute),
	}
	if err := validateCreatedPairing(valid, now); err != nil {
		t.Fatalf("validateCreatedPairing() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*pairing)
	}{
		{name: "wrong origin", mutate: func(value *pairing) { value.ActivationURL = "https://example.com/?start=pairing-id" }},
		{name: "javascript", mutate: func(value *pairing) { value.ActivationURL = "javascript:alert(1)" }},
		{name: "wrong start", mutate: func(value *pairing) { value.ActivationURL = "https://t.me/SigmoProBot?start=other" }},
		{name: "extra query", mutate: func(value *pairing) { value.ActivationURL = "https://t.me/SigmoProBot?start=pairing-id&next=evil" }},
		{name: "fragment", mutate: func(value *pairing) { value.ActivationURL = "https://t.me/SigmoProBot?start=pairing-id#fragment" }},
		{name: "invalid bot", mutate: func(value *pairing) { value.ActivationURL = "https://t.me/not-a-bot?start=pairing-id" }},
		{name: "expired", mutate: func(value *pairing) { value.ExpiresAt = now.Add(-time.Second) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := valid
			tt.mutate(&value)
			if err := validateCreatedPairing(value, now); err == nil {
				t.Fatal("validateCreatedPairing() error = nil")
			}
		})
	}
}

func TestControllerPrunesExpiredPairingSessions(t *testing.T) {
	controller, err := New(t.Context(), Config{Storage: openTestStorage(t)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	controller.pairings["expired"] = pairingSession{PollToken: "old", ExpiresAt: now.Add(-time.Second)}
	controller.pairings["active"] = pairingSession{PollToken: "new", ExpiresAt: now.Add(time.Minute)}

	controller.mu.Lock()
	controller.prunePairingsLocked(now)
	controller.mu.Unlock()

	if _, ok := controller.pairings["expired"]; ok {
		t.Fatal("expired pairing was not removed")
	}
	if _, ok := controller.pairings["active"]; !ok {
		t.Fatal("active pairing was removed")
	}
}

func TestStartRefreshesSignedLease(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	challengeValue := base64.RawURLEncoding.EncodeToString([]byte("one-time-challenge"))
	var controller *Controller
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/license-challenges":
			return response(http.StatusCreated, challenge{Challenge: challengeValue, ExpiresAt: time.Now().Add(time.Minute)}), nil
		case "/v1/license-leases":
			return response(http.StatusCreated, signedProof(t, privateKey, validLease(controller.identity.DeviceID, "Updated User"))), nil
		default:
			return response(http.StatusNotFound, errorResponse{ErrorCode: "resource_not_found", Message: "resource not found"}), nil
		}
	})}
	controller, err = New(t.Context(), Config{
		BaseURL: "https://license.example", LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
		Storage: openTestStorage(t), Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.saveLease(t.Context(), signedProof(t, privateKey, validLease(controller.identity.DeviceID, "Cached User"))); err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !controller.Authorized() || controller.Licensee().DisplayName != "Updated User" {
		t.Fatalf("Licensee() = %+v", controller.Licensee())
	}
}

func TestStartUsesOfflineLeaseForTransientFailure(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	})}
	controller, err := New(t.Context(), Config{
		BaseURL: "https://license.example", LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
		Storage: openTestStorage(t), Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.saveLease(t.Context(), signedProof(t, privateKey, validLease(controller.identity.DeviceID, "Offline User"))); err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(t.Context()); err == nil {
		t.Fatal("Start() error = nil, want transient network error")
	}
	if !controller.Authorized() {
		t.Fatal("Authorized() = false during offline validity")
	}
}

func TestStartDoesNotGraceExplicitRevocation(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusForbidden, errorResponse{ErrorCode: "license_entitlement_inactive", Message: "authorization revoked or expired"}), nil
	})}
	controller, err := New(t.Context(), Config{
		BaseURL: "https://license.example", LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
		Storage: openTestStorage(t), Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.saveLease(t.Context(), signedProof(t, privateKey, validLease(controller.identity.DeviceID, "Revoked User"))); err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(t.Context()); !errors.Is(err, errExplicitUnauthorized) {
		t.Fatalf("Start() error = %v", err)
	}
	if controller.Authorized() {
		t.Fatal("Authorized() = true after explicit revocation")
	}
}

func TestStartKeepsOfflineLeaseForGenericForbidden(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusForbidden, errorResponse{
			ErrorCode: "authorization_required",
			Message:   "authorization required",
		}), nil
	})}
	controller, err := New(t.Context(), Config{
		BaseURL:          "https://license.example",
		LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
		Storage:          openTestStorage(t),
		Client:           client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.saveLease(t.Context(), signedProof(t, privateKey, validLease(controller.identity.DeviceID, "Offline User"))); err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(t.Context()); err == nil || errors.Is(err, errExplicitUnauthorized) {
		t.Fatalf("Start() error = %v, want transient service error", err)
	}
	if !controller.Authorized() {
		t.Fatal("Authorized() = false for a generic upstream 403")
	}
}

func TestVerifyLeaseRejectsExcessiveValidity(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := New(t.Context(), Config{
		LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
		Storage:          openTestStorage(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*Lease)
	}{
		{
			name: "refresh delay",
			mutate: func(lease *Lease) {
				lease.RefreshAfter = lease.IssuedAt.Add(maxLeaseRefreshDelay + time.Second)
			},
		},
		{
			name: "lifetime",
			mutate: func(lease *Lease) {
				lease.ExpiresAt = lease.IssuedAt.Add(maxLeaseLifetime + time.Second)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lease := validLease(controller.identity.DeviceID, "Test User")
			tt.mutate(&lease)
			if _, err := controller.verifyLease(signedProof(t, privateKey, lease), time.Now()); err == nil {
				t.Fatal("verifyLease() error = nil")
			}
		})
	}
}

func TestRunExpiresLeaseWithoutWaitingForRetryInterval(t *testing.T) {
	restarted := make(chan struct{}, 1)
	controller, err := New(t.Context(), Config{
		Storage: openTestStorage(t),
		Restart: func() {
			restarted <- struct{}{}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	controller.setLease(&Lease{
		Status:       "active",
		RefreshAfter: now.Add(time.Hour),
		ExpiresAt:    now.Add(25 * time.Millisecond),
	}, nil)

	if err := controller.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("authorization expiry did not request a restart")
	}
	if controller.Authorized() {
		t.Fatal("Authorized() = true after lease expiry")
	}
}

func TestDoJSONPreservesWorkerError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusGone, errorResponse{
			ErrorCode: "license_pairing_expired",
			Message:   "pairing expired",
		}), nil
	})}
	controller, err := New(t.Context(), Config{
		BaseURL: "https://license.example",
		Storage: openTestStorage(t),
		Client:  client,
	})
	if err != nil {
		t.Fatal(err)
	}

	status, err := controller.doJSON(t.Context(), http.MethodGet, "/v1/license-pairings/pairing-id", nil, "poll-token", nil)
	if status != http.StatusGone {
		t.Fatalf("doJSON() status = %d, want %d", status, http.StatusGone)
	}
	var remote *serviceError
	if !errors.As(err, &remote) {
		t.Fatalf("doJSON() error = %v, want *serviceError", err)
	}
	if remote.ErrorCode != "license_pairing_expired" || remote.Message != "pairing expired" {
		t.Fatalf("doJSON() error = %+v", remote)
	}
}

func TestLatestVerifiesExactWorkerManifestBytes(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := appupdate.Manifest{
		SchemaVersion: appupdate.ManifestSchemaVersion,
		Edition:       "pro",
		Channel:       "dev",
		Version:       "dev-01234567",
		Commit:        "0123456789abcdef0123456789abcdef01234567",
		PublishedAt:   time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		Notes:         "test release",
		Artifacts: []appupdate.Artifact{{
			Target: "linux-amd64",
			Name:   "sigmo-pro-linux-amd64",
			Size:   10,
			SHA256: strings.Repeat("0", 64),
		}},
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestData = append(manifestData, '\n')
	signature := base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestData))
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/release-channels/dev/releases/latest" {
			return response(http.StatusNotFound, errorResponse{ErrorCode: "resource_not_found", Message: "resource not found"}), nil
		}
		return response(http.StatusOK, releaseResponse{
			Manifest:    string(manifestData),
			Signature:   signature,
			DownloadURL: "https://license.example/v1/downloads/ticket",
		}), nil
	})}
	controller, err := New(t.Context(), Config{
		BaseURL:          "https://license.example",
		ReleasePublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
		Storage:          openTestStorage(t),
		Client:           client,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := validLease(controller.identity.DeviceID, "Release User")
	controller.setLease(&lease, &signedLease{Lease: json.RawMessage(`{}`), Signature: "cached-proof"})

	release, err := controller.Latest(t.Context(), "dev", "linux-amd64")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !release.Verified || release.Manifest.Version != manifest.Version || release.Artifact.URL == "" {
		t.Fatalf("Latest() = %+v", release)
	}
}

func TestLatestRevocationClearsLeaseAndPreservesWorkerCode(t *testing.T) {
	restarted := make(chan struct{}, 1)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusForbidden, errorResponse{
			ErrorCode: "license_entitlement_inactive",
			Message:   "authorization revoked or expired",
		}), nil
	})}
	controller, err := New(t.Context(), Config{
		BaseURL: "https://license.example",
		Storage: openTestStorage(t),
		Client:  client,
		Restart: func() {
			restarted <- struct{}{}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := validLease(controller.identity.DeviceID, "Release User")
	controller.setLease(&lease, &signedLease{Lease: json.RawMessage(`{}`), Signature: "cached-proof"})

	_, err = controller.Latest(t.Context(), "dev", "linux-amd64")
	if !errors.Is(err, errExplicitUnauthorized) || appupdate.ErrorCode(err) != "license_entitlement_inactive" {
		t.Fatalf("Latest() error = %v, code = %q", err, appupdate.ErrorCode(err))
	}
	if controller.Authorized() {
		t.Fatal("Authorized() = true after explicit update authorization failure")
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("explicit update authorization failure did not request a restart")
	}
}

func TestDownloadRejectsUnexpectedOrigin(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return response(http.StatusOK, map[string]string{}), nil
	})}
	controller, err := New(t.Context(), Config{
		BaseURL: "https://license.example",
		Storage: openTestStorage(t),
		Client:  client,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := validLease(controller.identity.DeviceID, "Release User")
	controller.setLease(&lease, &signedLease{Lease: json.RawMessage(`{}`), Signature: "cached-proof"})

	_, err = controller.Download(t.Context(), appupdate.Release{Artifact: appupdate.Artifact{
		URL: "https://downloads.example/v1/downloads/ticket",
	}})
	if err == nil || !strings.Contains(err.Error(), "origin does not match") {
		t.Fatalf("Download() error = %v", err)
	}
	if called {
		t.Fatal("HTTP client was called for an unexpected origin")
	}
}

func validLease(deviceID, name string) Lease {
	issuedAt := time.Now().UTC().Add(-time.Minute)
	return Lease{
		SchemaVersion: 1, DeviceID: deviceID, TelegramID: 123456, Status: "active", DisplayName: name,
		IssuedAt: issuedAt, RefreshAfter: issuedAt.Add(24 * time.Hour), ExpiresAt: issuedAt.Add(72 * time.Hour),
	}
}

func signedProof(t *testing.T, privateKey ed25519.PrivateKey, lease Lease) signedLease {
	t.Helper()
	data, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	return signedLease{Lease: data, Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, data))}
}

func response(status int, value any) *http.Response {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode test response: %v", err))
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(data))),
	}
}

func openTestStorage(t *testing.T) *storage.Store {
	t.Helper()
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test storage: %v", err)
		}
	})
	return db
}
