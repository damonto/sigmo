package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/pkg/lpa"
)

func TestLPAErrorWritesOneProblem(t *testing.T) {
	for _, tt := range []struct {
		name   string
		cause  error
		status int
		code   string
	}{
		{"secure element required", lpa.ErrSERequired, http.StatusBadRequest, "se_required"},
		{"secure element absent", lpa.ErrSENotFound, http.StatusNotFound, "se_not_found"},
		{"unsupported card", lpa.ErrNoSupportedAID, http.StatusNotFound, "euicc_not_supported"},
		{"internal failure", errors.New("private diagnostic"), http.StatusInternalServerError, "delete_esim_failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(httptest.NewRequest(http.MethodDelete, "/", nil), rec)
			if err := LPAError(c, fmt.Errorf("acquire card: %w", tt.cause), "delete_esim_failed"); err != nil {
				t.Fatal(err)
			}
			var problem ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatalf("response must contain exactly one JSON document: %s, %v", rec.Body.String(), err)
			}
			if rec.Code != tt.status || problem.Status != tt.status || problem.ErrorCode != tt.code {
				t.Fatalf("response = %d, %+v", rec.Code, problem)
			}
			if tt.status == http.StatusInternalServerError && problem.Detail != "internal server error" {
				t.Fatalf("internal diagnostic exposed: %+v", problem)
			}
		})
	}
}
