package message

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
)

func TestWriteSendMessageError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantStatus     int
		wantErrorCode  string
		exposeInternal bool
	}{
		{
			name:          "invalid recipient",
			err:           errRecipientInvalid,
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: errorCodeRecipientInvalid,
		},
		{
			name:          "recipient required",
			err:           errRecipientRequired,
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: errorCodeRecipientRequired,
		},
		{
			name:          "text required",
			err:           errTextRequired,
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: errorCodeTextRequired,
		},
		{
			name:           "send failed",
			err:            errors.New("send SMS"),
			wantStatus:     http.StatusInternalServerError,
			wantErrorCode:  errorCodeSendMessageFailed,
			exposeInternal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpapi.SetExposeInternalErrors(tt.exposeInternal)
			defer httpapi.SetExposeInternalErrors(false)

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
		})
	}
}
