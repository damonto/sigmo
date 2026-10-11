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
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/modems/device/internet-connections?ticket=secret", nil)
	req.Header.Set(echo.HeaderXRequestID, "request-1")
	c, rec := echotest.ContextConfig{Request: req, PathValues: echo.PathValues{{Name: "id", Value: "device"}}}.ToContextRecorder(t)
	if err := Internal(c, "connect_internet_failed", errors.New("modem connection rejected")); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "internal server error") || strings.Contains(rec.Body.String(), "modem connection rejected") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	for _, want := range []string{`msg="http internal error"`, `error="modem connection rejected"`, "error_code=", "request_id=request-1", "method=POST", "imei=device"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs = %s, want %q", logs.String(), want)
		}
	}
	if strings.Contains(logs.String(), "ticket=secret") {
		t.Fatalf("query leaked into logs: %s", logs.String())
	}
}
