package authpage

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/pkg/locale"
)

func TestWriteRendersLocalizedResultWithoutScripts(t *testing.T) {
	for _, test := range []struct {
		name      string
		language  locale.Tag
		succeeded bool
		want      string
	}{
		{name: "English failure", want: "did not complete"},
		{name: "English success", succeeded: true, want: "test succeeded"},
		{name: "Chinese failure", language: locale.Chinese, want: "测试未完成"},
		{name: "Chinese success", language: locale.Chinese, succeeded: true, want: "测试成功"},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := echo.New()
			rec := httptest.NewRecorder()
			err := Write(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec), test.succeeded, test.language)
			if err != nil {
				t.Fatal(err)
			}
			body := rec.Body.String()
			if rec.Code != http.StatusOK || !strings.Contains(body, test.want) || strings.Contains(body, "<script") {
				t.Fatalf("status=%d body=%s", rec.Code, body)
			}
			if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'none'") {
				t.Fatalf("Content-Security-Policy = %q", rec.Header().Get("Content-Security-Policy"))
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
			}
		})
	}
}
