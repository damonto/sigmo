package modem

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestUnlockSIMError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantBody string
	}{
		{
			name:     "pin required",
			err:      mmodem.ErrSIMPinRequired,
			wantCode: http.StatusBadRequest,
			wantBody: errorCodeUnlockSIMInvalidRequest,
		},
		{
			name:     "not required",
			err:      mmodem.ErrSIMUnlockNotRequired,
			wantCode: http.StatusBadRequest,
			wantBody: errorCodeUnlockSIMNotRequired,
		},
		{
			name:     "enable failed after unlock",
			err:      mmodem.ErrEnableAfterSIMUnlock,
			wantCode: http.StatusInternalServerError,
			wantBody: errorCodeEnableModemAfterUnlockFailed,
		},
		{
			name:     "send pin failed",
			err:      errors.New("bad pin"),
			wantCode: http.StatusInternalServerError,
			wantBody: errorCodeUnlockSIMFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/modems/modem-1/sim-unlocks", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if err := unlockSIMError(c, tt.err); err != nil {
				t.Fatalf("unlockSIMError() error = %v", err)
			}
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			var got httpapi.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.ErrorCode != tt.wantBody {
				t.Fatalf("error_code = %q, want %q", got.ErrorCode, tt.wantBody)
			}
		})
	}
}
