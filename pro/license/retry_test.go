package license

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

func TestRetryableAuthorizationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "network unreachable", err: &url.Error{Op: "Post", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ENETUNREACH}}, want: true},
		{name: "DNS unavailable", err: &net.DNSError{Err: "server unavailable", IsTemporary: true}, want: true},
		{name: "request timeout", err: context.DeadlineExceeded, want: true},
		{name: "connection closed", err: io.EOF, want: true},
		{name: "truncated response", err: io.ErrUnexpectedEOF, want: true},
		{name: "canceled", err: context.Canceled},
		{name: "configuration", err: errors.New("service is not configured")},
		{name: "local client error", err: &url.Error{Op: "Post", Err: errors.New("invalid request")}},
		{name: "invalid certificate", err: &url.Error{Op: "Post", Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}}},
		{name: "rate limited", err: &serviceError{StatusCode: http.StatusTooManyRequests}, want: true},
		{name: "service unavailable", err: &serviceError{StatusCode: http.StatusServiceUnavailable}, want: true},
		{name: "gateway timeout", err: &serviceError{StatusCode: http.StatusGatewayTimeout}, want: true},
		{name: "bad request", err: &serviceError{StatusCode: http.StatusBadRequest}},
		{name: "generic forbidden", err: &serviceError{StatusCode: http.StatusForbidden, ErrorCode: "authorization_required"}},
		{name: "not implemented", err: &serviceError{StatusCode: http.StatusNotImplemented}},
		{name: "expired challenge", err: &serviceError{StatusCode: http.StatusGone, ErrorCode: "license_challenge_expired"}, want: true},
		{name: "consumed challenge", err: &serviceError{StatusCode: http.StatusForbidden, ErrorCode: "license_challenge_invalid"}, want: true},
		{name: "revoked entitlement", err: &serviceError{StatusCode: http.StatusForbidden, ErrorCode: "license_entitlement_inactive"}},
		{name: "explicit rejection with unexpected status", err: &serviceError{StatusCode: http.StatusServiceUnavailable, ErrorCode: "license_device_unauthorized"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryableAuthorizationError(tt.err); got != tt.want {
				t.Fatalf("retryableAuthorizationError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAuthorizationRetryDelayUsesBoundedJitter(t *testing.T) {
	for _, backoff := range []time.Duration{startupRefreshRetryInitialDelay, startupRefreshRetryMaxDelay} {
		t.Run(backoff.String(), func(t *testing.T) {
			delay, retry := authorizationRetryDelay(io.ErrUnexpectedEOF, backoff)
			if !retry || delay < backoff/2 || delay >= backoff {
				t.Fatalf("authorizationRetryDelay() = %v, %v; want delay in [%v, %v)", delay, retry, backoff/2, backoff)
			}
		})
	}
}

func TestStartReturnsPermanentFailures(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		prepare      func(*testing.T, *Controller)
		invalidLease bool
		wantRequests int
	}{
		{
			name:    "missing service URL",
			prepare: func(_ *testing.T, c *Controller) { c.baseURL = "" },
		},
		{
			name:    "missing public key",
			prepare: func(_ *testing.T, c *Controller) { c.licensePublicKey = nil },
		},
		{
			name: "closed database",
			prepare: func(t *testing.T, c *Controller) {
				if err := c.storage.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:         "invalid signature",
			prepare:      func(_ *testing.T, c *Controller) { c.licensePublicKey = otherPublicKey },
			wantRequests: 2,
		},
		{
			name:         "invalid signed metadata",
			invalidLease: true,
			wantRequests: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			var controller *Controller
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Path == "/v1/license-challenges" {
					return response(http.StatusCreated, challenge{
						Challenge: base64.RawURLEncoding.EncodeToString([]byte("challenge")),
						ExpiresAt: time.Now().Add(time.Minute),
					}), nil
				}
				lease := validLease(controller.identity.DeviceID, "Test User")
				if tt.invalidLease {
					lease.DeviceID = "another-device"
				}
				return response(http.StatusCreated, signedProof(t, privateKey, lease)), nil
			})}
			controller, err = newTestController(t, Config{
				BaseURL: "https://license.example", LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
				Storage: openTestStorage(t), Client: client,
			})
			if err != nil {
				t.Fatal(err)
			}
			session := validSession(t)
			installSession(t, controller, session)
			if tt.prepare != nil {
				tt.prepare(t, controller)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := controller.Start(ctx)
			if err == nil || ctx.Err() != nil || errors.Is(err, errExplicitUnauthorized) {
				t.Fatalf("Start() error = %v, want immediate operational failure", err)
			}
			if controller.Authorized() || requests != tt.wantRequests {
				t.Fatalf("Start() authorized = %v, requests = %d; want false and %d", controller.Authorized(), requests, tt.wantRequests)
			}
			current := controller.currentSession()
			if current == nil || current.SessionID != session.SessionID || current.Generation != session.Generation || current.RefreshToken != session.RefreshToken {
				t.Fatal("permanent startup failure changed the device session")
			}
		})
	}
}

func TestStartWithoutSessionRequiresActivation(t *testing.T) {
	controller, err := newTestController(t, Config{Storage: openTestStorage(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if controller.Authorized() {
		t.Fatal("Authorized() = true without a device session")
	}
}

func TestStartCancellationDuringBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		publicKey, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		requests := 0
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return response(http.StatusServiceUnavailable, errorResponse{ErrorCode: "service_unavailable"}), nil
		})}
		controller, err := newTestController(t, Config{
			BaseURL: "https://license.example", LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
			Storage: openTestStorage(t), Client: client,
		})
		if err != nil {
			t.Fatal(err)
		}
		installSession(t, controller, validSession(t))
		ctx, cancel := context.WithCancel(t.Context())
		var wg sync.WaitGroup
		defer func() {
			cancel()
			wg.Wait()
		}()
		var startErr error
		wg.Go(func() { startErr = controller.Start(ctx) })
		synctest.Wait()
		if requests != 1 {
			t.Fatalf("requests before cancellation = %d, want 1", requests)
		}
		cancel()
		wg.Wait()
		if !errors.Is(startErr, context.Canceled) || requests != 1 {
			t.Fatalf("Start() error = %v, requests = %d; want cancellation without another request", startErr, requests)
		}
	})
}

func TestStartHonorsRetryAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		publicKey, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		requests := 0
		var firstRequest time.Time
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			if requests == 1 {
				firstRequest = time.Now()
				resp := response(http.StatusTooManyRequests, errorResponse{ErrorCode: "rate_limited"})
				resp.Header.Set("Retry-After", "90")
				return resp, nil
			}
			if elapsed := time.Since(firstRequest); elapsed != 90*time.Second {
				t.Fatalf("retry after %v, want 90s", elapsed)
			}
			return response(http.StatusForbidden, errorResponse{ErrorCode: "license_entitlement_inactive"}), nil
		})}
		controller, err := newTestController(t, Config{
			BaseURL: "https://license.example", LicensePublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
			Storage: openTestStorage(t), Client: client,
		})
		if err != nil {
			t.Fatal(err)
		}
		installSession(t, controller, validSession(t))
		if err := controller.Start(t.Context()); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if requests != 2 || controller.Authorized() {
			t.Fatalf("Start() requests = %d, authorized = %v; want 2 and activation required", requests, controller.Authorized())
		}
	})
}
