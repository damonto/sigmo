package license

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
)

func TestPairingSurvivesUnrelatedUpstreamErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		code       string
		wantStatus int
		wantGone   bool
	}{
		{name: "unknown route", status: 404, code: "resource_not_found", wantStatus: 502},
		{name: "wrong method", status: 405, code: "method_not_allowed", wantStatus: 502},
		{name: "unrelated expired resource", status: 410, code: "license_challenge_expired", wantStatus: 502},
		{name: "wrong pairing status", status: 503, code: "license_pairing_not_found", wantStatus: 503},
		{name: "pairing missing", status: 404, code: "license_pairing_not_found", wantStatus: 404, wantGone: true},
		{name: "pairing expired", status: 410, code: "license_pairing_expired", wantStatus: 410, wantGone: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(tt.status, errorResponse{ErrorCode: tt.code, Message: "private upstream details"}), nil
			})}
			controller, err := newTestController(t, Config{BaseURL: "https://license.example", Storage: openTestStorage(t), Client: client})
			if err != nil {
				t.Fatal(err)
			}
			controller.pairings["pending"] = pairingSession{PollToken: "poll-token", ExpiresAt: time.Now().Add(time.Minute)}
			e := echo.New()
			e.HTTPErrorHandler = httpapi.HTTPErrorHandler
			controller.RegisterRoutes(e.Group("/api/v1"))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/license/pairings/pending", nil))
			if rec.Code != tt.wantStatus {
				t.Fatalf("HTTP status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if _, exists := controller.pairings["pending"]; exists == tt.wantGone {
				t.Fatalf("pairing exists = %v, want %v", exists, !tt.wantGone)
			}
		})
	}
}
