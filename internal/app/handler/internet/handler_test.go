package internet

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	internetcore "github.com/damonto/sigmo/internal/pkg/internet"
)

func TestModemLookupError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		code       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "not found",
			err:        errModemNotFound,
			code:       "current_failed",
			wantStatus: http.StatusNotFound,
			wantBody:   errorCodeModemNotFound,
		},
		{
			name:       "internal",
			err:        errors.New("dbus unavailable"),
			code:       "current_failed",
			wantStatus: http.StatusInternalServerError,
			wantBody:   "current_failed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			h := &Handler{}

			if err := h.modemLookupError(c, tt.err, tt.code); err != nil {
				t.Fatalf("modemLookupError() error = %v", err)
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

func TestInternetError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "unsupported ip method",
			err:        internetcore.ErrUnsupportedIPMethod,
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   errorCodeUnsupportedInternetConfiguration,
		},
		{
			name:       "internal connect error",
			err:        errors.New("permission denied"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   errorCodeConnectInternetFailed,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if err := internetError(c, tt.err, errorCodeConnectInternetFailed); err != nil {
				t.Fatalf("internetError() error = %v", err)
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

func TestResponseFromPublic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info internetcore.IPInfo
		want PublicResponse
	}{
		{
			name: "public info",
			info: internetcore.IPInfo{
				IP:           "209.9.201.161",
				Country:      "HK",
				Organization: "AS4760 HKT Limited",
			},
			want: PublicResponse{
				IP:           "209.9.201.161",
				Country:      "HK",
				Organization: "AS4760 HKT Limited",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := responseFromPublic(tt.info)
			if got != tt.want {
				t.Fatalf("responseFromPublic() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
