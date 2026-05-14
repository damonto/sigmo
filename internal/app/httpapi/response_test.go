package httpapi

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/echotest"
)

func TestInternalLogsError(t *testing.T) {
	tests := []struct {
		name     string
		expose   bool
		wantBody string
	}{
		{
			name:     "internal production",
			expose:   false,
			wantBody: "internal server error",
		},
		{
			name:     "internal debug",
			expose:   true,
			wantBody: "modem connection rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetExposeInternalErrors(tt.expose)
			t.Cleanup(func() {
				SetExposeInternalErrors(false)
			})

			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(previous)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/modems/860588043408833/internet-connections", nil)
			req.Header.Set(echo.HeaderXRequestID, "request-1")
			c, rec := echotest.ContextConfig{
				Request: req,
				PathValues: echo.PathValues{
					{Name: "id", Value: "860588043408833"},
				},
			}.ToContextRecorder(t)

			err := errors.New("modem connection rejected")
			if err := Internal(c, "connect_internet_failed", err); err != nil {
				t.Fatalf("write response: %v", err)
			}
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %s, want it to contain %q", rec.Body.String(), tt.wantBody)
			}

			for _, want := range []string{
				"msg=\"http internal error\"",
				"error=\"modem connection rejected\"",
				"error_code=",
				"request_id=request-1",
				"method=POST",
				"modem=860588043408833",
			} {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("logs = %s, want it to contain %q", logs.String(), want)
				}
			}
		})
	}
}
