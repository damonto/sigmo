package message

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	pmessage "github.com/damonto/sigmo/internal/pkg/message"
)

func TestWriteSendMessageError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantStatus    int
		wantErrorCode string
	}{
		{
			name:          "invalid recipient",
			err:           pmessage.ErrRecipientInvalid,
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "invalid_recipient",
		},
		{
			name:          "recipient required",
			err:           pmessage.ErrRecipientRequired,
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "recipient_required",
		},
		{
			name:          "text required",
			err:           pmessage.ErrTextRequired,
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "text_required",
		},
		{
			name:          "message route disconnected",
			err:           pmessage.ErrRouteNotConnected,
			wantStatus:    http.StatusServiceUnavailable,
			wantErrorCode: "message_route_not_connected",
		},
		{
			name:          "send failed",
			err:           errors.New("send SMS"),
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "send_message_failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/modems/1/messages", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if err := writeSendMessageError(c, tt.err); err != nil {
				t.Fatalf("writeSendMessageError() error = %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var got httpapi.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if got.ErrorCode != tt.wantErrorCode {
				t.Fatalf("error code = %q, want %q", got.ErrorCode, tt.wantErrorCode)
			}
			if tt.wantStatus == http.StatusInternalServerError && got.Detail != "internal server error" {
				t.Fatalf("detail = %q, want redacted internal error", got.Detail)
			}
		})
	}
}
