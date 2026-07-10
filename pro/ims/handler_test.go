//go:build ims

package ims

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type fakeModemFinder struct {
	modem *mmodem.Modem
}

func (f fakeModemFinder) Find(context.Context, string) (*mmodem.Modem, error) {
	return f.modem, nil
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
