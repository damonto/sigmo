package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/damonto/sigmo/internal/app/handler/capability"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/labstack/echo/v5"
)

func TestFeatureAuthorizationChangesWithoutRestart(t *testing.T) {
	authorized := false
	policy := feature.NewPolicy(func() bool { return authorized })
	e := echo.New()
	e.HTTPErrorHandler = httpapi.HTTPErrorHandler
	e.GET("/capabilities", capability.New([]feature.Name{feature.WiFiCalling, feature.Contacts, feature.NativeCalls, feature.CallHistory}, policy).List)
	e.GET("/licensed", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) }, RequireFeature(policy, feature.WiFiCalling))
	for _, allowed := range []bool{false, true, false} {
		authorized = allowed
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/licensed", nil))
		want := http.StatusForbidden
		features := `{"features":["callHistory","contacts","nativeCalls"]}` + "\n"
		if allowed {
			want = http.StatusNoContent
			features = `{"features":["callHistory","contacts","nativeCalls","wifiCalling"]}` + "\n"
		}
		if rec.Code != want {
			t.Fatalf("authorized=%v: HTTP %d, want %d", allowed, rec.Code, want)
		}
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != features {
			t.Fatalf("capabilities = %d %s", rec.Code, rec.Body)
		}
	}
}
