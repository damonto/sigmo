package esim

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	sgp22 "github.com/damonto/euicc-go/v2"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
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
			err:        errProfileAlreadyActive,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "profile not found is bad request",
			err:        errProfileNotFound,
			wantStatus: http.StatusBadRequest,
			wantBody:   errorCodeESIMProfileNotFound,
		},
		{
			name:       "internal error",
			err:        errors.New("lpa unavailable"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   errorCodeEnableESIMFailed,
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
			name:       "deadline exceeded maps to request timeout",
			err:        context.DeadlineExceeded,
			wantStatus: http.StatusRequestTimeout,
			wantBody:   errorCodeEnableESIMTimeout,
		},
		{
			name:       "internal error",
			err:        errors.New("enable failed"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   errorCodeEnableESIMFailed,
		},
		{
			name:       "cat busy maps to conflict",
			err:        sgp22.ErrCatBusy,
			wantStatus: http.StatusConflict,
			wantBody:   errorCodeEnableESIMBusy,
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
		})
	}
}

func TestRestoreInternetBeforeProfileEnable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modem   *mmodem.Modem
		wantErr bool
	}{
		{
			name: "skip current SIM internet restore while modem is locked",
			modem: &mmodem.Modem{
				Status: wwanmodem.Status{SIM: wwanmodem.SIMStateLocked},
			},
		},
		{
			name:    "unlocked modem requires internet connector",
			modem:   &mmodem.Modem{Status: wwanmodem.Status{Power: wwanmodem.PowerStateOn, Registration: wwanmodem.RegistrationHome}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := &Handler{}
			err := h.restoreInternetBeforeProfileEnable(t.Context(), tt.modem)
			if (err != nil) != tt.wantErr {
				t.Fatalf("restoreInternetBeforeProfileEnable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
