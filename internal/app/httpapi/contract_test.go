package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/damonto/sigmo/internal/pkg/apierror"
	"github.com/damonto/sigmo/internal/pkg/validator"
)

func testAPI() *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = HTTPErrorHandler
	e.Use(ResponseHeaders, middleware.RequestID(), middleware.Recover())
	return e
}

func TestHTTPFailuresUseProblemDetails(t *testing.T) {
	e := testAPI()
	e.POST("/api/v1/items", func(c *echo.Context) error { return c.NoContent(204) })
	e.GET("/api/v1/missing", func(c *echo.Context) error { return NotFound(c, "modem_not_found", errors.New("missing modem")) })
	e.GET("/api/v1/private", func(c *echo.Context) error { return Unauthorized(c, "invalid_token", errors.New("login expired")) })
	e.GET("/api/v1/panic", func(c *echo.Context) error { panic("private database details") })
	for _, test := range []struct {
		name, method, path, code string
		status                   int
		allow, auth              bool
	}{
		{"route", "GET", "/api/v1/absent", "resource_not_found", 404, false, false},
		{"method", "GET", "/api/v1/items", "method_not_allowed", 405, true, false},
		{"business", "GET", "/api/v1/missing", "modem_not_found", 404, false, false},
		{"login", "GET", "/api/v1/private", "invalid_token", 401, false, true},
		{"panic", "GET", "/api/v1/panic", "internal_server_error", 500, false, false},
		{"head", "HEAD", "/api/v1/absent", "", 404, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(test.method, test.path, nil))
			if rec.Code != test.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), apierror.MediaType) || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("headers=%v", rec.Header())
			}
			if test.allow != (rec.Header().Get("Allow") != "") || test.auth != (rec.Header().Get("WWW-Authenticate") != "") {
				t.Fatalf("resource headers=%v", rec.Header())
			}
			if test.method == "HEAD" {
				if rec.Body.Len() != 0 {
					t.Fatalf("HEAD body=%s", rec.Body.String())
				}
				return
			}
			var problem apierror.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if problem.ErrorCode != test.code || problem.Type != apierror.TypePrefix+test.code || problem.RequestID == "" || problem.RequestID != rec.Header().Get("X-Request-ID") {
				t.Fatalf("problem=%+v", problem)
			}
			if strings.Contains(rec.Body.String(), "private database details") || strings.Contains(rec.Body.String(), `"message"`) {
				t.Fatalf("invalid external details: %s", rec.Body.String())
			}
		})
	}
}

func TestJSONRejectionStopsBusinessLogic(t *testing.T) {
	for _, test := range []struct {
		name, body, media string
		status            int
		unknownLength     bool
	}{
		{"valid", `{"name":"ok"}`, "application/json", 204, false},
		{"empty", "", "application/json", 400, false},
		{"broken", `{`, "application/json", 400, false},
		{"trailing document", `{} {}`, "application/json", 400, false},
		{"media", `{}`, "text/plain", 415, false},
		{"invalid field", `{"name":1}`, "application/json", 422, false},
		{"null", `null`, "application/json", 422, false},
		{"padded null", " \nnull\t", "application/json", 422, false},
		{"validation", `{}`, "application/json", 422, false},
		{"declared size", strings.Repeat("x", apierror.MaxBodySize+1), "application/json", 413, false},
		{"streamed size", strings.Repeat("x", apierror.MaxBodySize+1), "application/json", 413, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := testAPI()
			var err error
			e.Validator, err = validator.New()
			if err != nil {
				t.Fatal(err)
			}
			called := false
			e.POST("/api/v1/items", func(c *echo.Context) error {
				var req struct {
					Name string `json:"name" validate:"required"`
				}
				if err := BindAndValidate(c, &req, "invalid_request"); err != nil {
					return err
				}
				called = true
				return c.NoContent(204)
			})
			req := httptest.NewRequest("POST", "/api/v1/items", strings.NewReader(test.body))
			req.Header.Set("Content-Type", test.media)
			if test.unknownLength {
				req.ContentLength = -1
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != test.status || called != (test.status == 204) {
				t.Fatalf("status=%d called=%v body=%s", rec.Code, called, rec.Body.String())
			}
		})
	}
}

func TestNullJSONDoesNotApplyZeroValueSettings(t *testing.T) {
	e := testAPI()
	name := "configured name"
	e.PATCH("/api/v1/settings", func(c *echo.Context) error {
		var req struct {
			Name string `json:"name"`
		}
		if err := BindJSON(c, &req, "invalid_request"); err != nil {
			return err
		}
		name = req.Name
		return c.NoContent(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader("null"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity || name != "configured name" {
		t.Fatalf("status = %d, name = %q; want 422 with unchanged settings", rec.Code, name)
	}
}

func TestUpstreamMappingPreservesLocalResourceAndLogin(t *testing.T) {
	for _, test := range []struct {
		name, remoteCode, code string
		remoteStatus, status   int
		invalid                bool
	}{
		{"method", "method_not_allowed", "upstream_contract_error", 405, 502, false},
		{"route", "resource_not_found", "upstream_contract_error", 404, 502, false},
		{"pairing", "license_pairing_not_found", "license_pairing_not_found", 404, 404, false},
		{"pairing expired", "license_pairing_expired", "license_pairing_expired", 410, 410, false},
		{"request", "invalid_request", "upstream_request_invalid", 422, 502, false},
		{"authentication", "authorization_required", "upstream_authentication_failed", 401, 502, false},
		{"lease", "license_lease_expired", "license_lease_expired", 401, 403, false},
		{"quota", "license_pairing_limit_exceeded", "license_pairing_limit_exceeded", 429, 429, false},
		{"maintenance", "database_maintenance", "upstream_unavailable", 503, 503, false},
		{"wrong auth status", "license_device_unauthorized", "upstream_unavailable", 503, 503, false},
		{"malformed", "", "upstream_response_invalid", 401, 502, true},
		{"timeout", "gateway_timeout", "upstream_timeout", 504, 504, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			remote := &apierror.Error{StatusCode: test.remoteStatus, ErrorCode: test.remoteCode, Message: "private upstream details", RequestID: "upstream-request", Allow: "DELETE", Authenticate: "SigmoLease", RetryAfter: "90", RetryAt: time.Now().Add(90 * time.Second)}
			if test.invalid {
				remote.ParseErr = errors.New("invalid JSON")
			}
			e := testAPI()
			e.GET("/api/v1/check", func(c *echo.Context) error { return UpstreamError(c, "update_check_failed", remote) })
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/check", nil))
			var problem apierror.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if rec.Code != test.status || problem.ErrorCode != test.code {
				t.Fatalf("status=%d problem=%+v", rec.Code, problem)
			}
			if problem.RequestID == "upstream-request" || rec.Header().Get("Allow") != "" || rec.Header().Get("WWW-Authenticate") != "" {
				t.Fatalf("leaked upstream metadata: %v", rec.Header())
			}
			if (rec.Header().Get("Retry-After") != "") != (test.status == 429 || test.status == 503) {
				t.Fatalf("retry headers=%v", rec.Header())
			}
			if strings.Contains(rec.Body.String(), remote.Message) {
				t.Fatal("upstream detail escaped the adapter")
			}
		})
	}
}
