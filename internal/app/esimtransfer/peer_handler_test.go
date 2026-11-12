package esimtransfer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
)

func TestPeerDecisionRejectsInvalidJSONBeforeApplyingDecision(t *testing.T) {
	for _, tt := range []struct {
		name, body, mediaType string
		status                int
	}{
		{"null", "null", "application/json", http.StatusUnprocessableEntity},
		{"missing body", "", "application/json", http.StatusBadRequest},
		{"wrong field type", `{"accept":"true"}`, "application/json", http.StatusUnprocessableEntity},
		{"form body", "accept=true", "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Rejected input must not reach a broker or apply an approval.
			h := &Handler{}
			e := echo.New()
			e.HTTPErrorHandler = httpapi.HTTPErrorHandler
			e.POST("/requests/:id/decisions", h.PeerDecision)
			req := httptest.NewRequest(http.MethodPost, "/requests/request/decisions", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.mediaType)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			var problem httpapi.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tt.status || problem.Status != tt.status {
				t.Fatalf("response = %d, %+v", rec.Code, problem)
			}
		})
	}
}
