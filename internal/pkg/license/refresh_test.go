package license

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/damonto/sigmo/internal/pkg/carrierconfig"
)

func TestPairingReplacesCatalogAndIgnoresOldRevocation(t *testing.T) {
	controller, err := newTestController(t, Config{Storage: openTestStorage(t)})
	if err != nil {
		t.Fatal(err)
	}
	previous := validSession(t)
	installSession(t, controller, previous)
	controller.configuration.current = &carrierconfig.Config{}
	controller.configuration.grant.SessionID = previous.SessionID
	controller.configuration.denied = true
	next := previous
	next.SessionID = "new-pairing-session"
	installSession(t, controller, next)
	lease := validLease(controller.identity.DeviceID, "Test")
	lease.SessionID = next.SessionID
	controller.setLease(&lease, &signedLease{})
	if controller.configuration.current != nil || controller.configuration.denied {
		t.Fatal("new pairing retained the old catalog or revocation")
	}
	if err := os.WriteFile(controller.configuration.path, []byte("encrypted-cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller.revokeConfiguration(&previous)
	if !controller.Authorized() || controller.configuration.denied {
		t.Fatal("old refresh revoked the new pairing")
	}
	if _, err := os.Stat(controller.configuration.path); err != nil {
		t.Fatal("old refresh removed the new cache:", err)
	}
	controller.revokeConfiguration(&next)
	if controller.Authorized() || !controller.configuration.denied {
		t.Fatal("current revocation was not applied")
	}
	if _, err := os.Stat(controller.configuration.path); !os.IsNotExist(err) {
		t.Fatal("revoked cache remains:", err)
	}
}

func TestAuthorizationRefreshRunsInBackground(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int32
		controller, err := newTestController(t, Config{
			Storage: openTestStorage(t), BaseURL: "https://license.example",
			LicensePublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				requests.Add(1)
				return nil, errors.New("offline")
			})},
		})
		if err != nil {
			t.Fatal(err)
		}
		installSession(t, controller, validSession(t))
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- controller.Run(ctx) }()
		synctest.Wait()
		if requests.Load() != 1 {
			t.Fatalf("initial refresh requests = %d", requests.Load())
		}
		time.Sleep(31 * time.Second)
		if requests.Load() != 2 {
			t.Fatalf("retry requests = %d", requests.Load())
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}
