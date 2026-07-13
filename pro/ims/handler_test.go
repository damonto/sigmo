//go:build ims

package ims

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	appvalidator "github.com/damonto/sigmo/internal/pkg/validator"
)

type fakeModemFinder struct {
	modem *mmodem.Modem
}

func (f fakeModemFinder) Find(context.Context, string) (*mmodem.Modem, error) {
	return f.modem, nil
}

type updateCoordinatorProbe struct {
	Coordinator
	updated bool
}

func (p *updateCoordinatorProbe) UpdateSettings(context.Context, *mmodem.Modem, Settings) error {
	p.updated = true
	return nil
}

func TestUpdateVoLTESettingsValidatesManagedDevice(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		device      *fakeManagedVoLTEDevice
		openErr     error
		wantStatus  int
		wantUpdated bool
		wantOpened  bool
		wantCalls   []string
	}{
		{
			name:       "rejects unavailable device",
			body:       `{"enabled":true}`,
			openErr:    wwan.ErrUnsupported,
			wantStatus: http.StatusBadRequest,
			wantOpened: true,
		},
		{
			name:        "accepts WDS path without IMSA",
			body:        `{"enabled":true}`,
			device:      &fakeManagedVoLTEDevice{},
			wantStatus:  http.StatusNoContent,
			wantUpdated: true,
			wantOpened:  true,
			wantCalls:   []string{"status", "ims-profile", "packet-service"},
		},
		{
			name:        "disable skips validation",
			body:        `{"enabled":false}`,
			openErr:     wwan.ErrUnsupported,
			wantStatus:  http.StatusNoContent,
			wantUpdated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousOpen := openManagedVoLTEDevice
			opened := false
			openManagedVoLTEDevice = func(*mmodem.Modem) (managedVoLTEDevice, error) {
				opened = true
				return tt.device, tt.openErr
			}
			t.Cleanup(func() {
				openManagedVoLTEDevice = previousOpen
			})

			volte := &updateCoordinatorProbe{}
			h := &Handler{
				registry: fakeModemFinder{modem: &mmodem.Modem{EquipmentIdentifier: "modem-1"}},
				volte:    volte,
			}
			e := echo.New()
			e.Validator = appvalidator.New()
			e.PUT("/modems/:id/volte/settings", h.UpdateVoLTESettings)
			req := httptest.NewRequest(http.MethodPut, "/modems/modem-1/volte/settings", strings.NewReader(tt.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if volte.updated != tt.wantUpdated {
				t.Fatalf("UpdateSettings called = %v, want %v", volte.updated, tt.wantUpdated)
			}
			if opened != tt.wantOpened {
				t.Fatalf("openManagedVoLTEDevice called = %v, want %v", opened, tt.wantOpened)
			}
			var calls []string
			if tt.device != nil {
				calls = tt.device.calls
			}
			if !slices.Equal(calls, tt.wantCalls) {
				t.Fatalf("device calls = %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}

func TestDeleteSessionRouteDisconnectsCurrentSession(t *testing.T) {
	cancelled := false
	wifiCalling := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				cancel: func() {
					cancelled = true
				},
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	e := echo.New()
	h := &Handler{
		registry: fakeModemFinder{
			modem: &mmodem.Modem{EquipmentIdentifier: "modem-1"},
		},
		wifiCalling: wifiCalling,
	}
	e.DELETE("/modems/:id/wifi-calling/sessions/current", h.DeleteSession)

	req := httptest.NewRequest(http.MethodDelete, "/modems/modem-1/wifi-calling/sessions/current", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if !cancelled {
		t.Fatal("session was not cancelled")
	}
	if _, ok := wifiCalling.sessions["modem-1"]; ok {
		t.Fatal("session was not removed")
	}

	req = httptest.NewRequest(http.MethodDelete, "/modems/modem-1/wifi-calling/sessions/current", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("repeat status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}
