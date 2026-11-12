package esim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/labstack/echo/v5"

	appesim "github.com/damonto/sigmo/internal/app/esim"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestEnablePrepareError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "already active is idempotent success",
			err:        appesim.ErrProfileAlreadyActive,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "profile not found is bad request",
			err:        appesim.ErrProfileNotFound,
			wantStatus: http.StatusBadRequest,
			wantBody:   "esim_profile_not_found",
		},
		{
			name:       "internal error",
			err:        errors.New("lpa unavailable"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   "enable_esim_failed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := echo.New()
			req := httptest.NewRequest(http.MethodPut, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if err := enablePrepareError(c, tt.err); err != nil {
				t.Fatalf("enablePrepareError() error = %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %s, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestEnableError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "physical removal takes precedence over operation cancellation",
			err:        errors.Join(mmodem.ErrNotFound, context.Canceled),
			wantStatus: http.StatusNotFound,
			wantBody:   "modem_not_found",
		},
		{
			name:       "missing secure element maps to one not found response",
			err:        lpa.ErrSENotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   "se_not_found",
		},
		{
			name:       "secure element required maps to one bad request response",
			err:        lpa.ErrSERequired,
			wantStatus: http.StatusBadRequest,
			wantBody:   "se_required",
		},
		{
			name:       "deadline exceeded maps to request timeout",
			err:        context.DeadlineExceeded,
			wantStatus: http.StatusRequestTimeout,
			wantBody:   "esim_enable_timeout",
		},
		{
			name:       "internal error",
			err:        errors.New("enable failed"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   "enable_esim_failed",
		},
		{
			name:       "cat busy maps to conflict",
			err:        sgp22.ErrCatBusy,
			wantStatus: http.StatusConflict,
			wantBody:   "esim_enable_busy",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := echo.New()
			req := httptest.NewRequest(http.MethodPut, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if err := enableError(c, tt.err); err != nil {
				t.Fatalf("enableError() error = %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %s, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
			var problem httpapi.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatalf("invalid problem response: %s, %v", rec.Body.String(), err)
			}
		})
	}
}
