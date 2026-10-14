//go:build esim_transfer || ims

package websheet

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestCapabilityIsolationAndPolicyRevocation(t *testing.T) {
	policy := "first"
	broker := New(Config{AllowPrivateHosts: true, PolicyID: func() string { return policy }})
	carrier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range []string{"Authorization", "Cookie", "Cf-Access-Jwt-Assertion", "Cf-Access-Client-Secret"} {
			if r.Header.Get(name) != "" {
				t.Errorf("carrier received %s", name)
			}
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Referrer-Policy", "unsafe-url")
		if _, err := w.Write([]byte(`<html><a href="/next">next</a></html>`)); err != nil {
			t.Error(err)
		}
	}))
	defer carrier.Close()
	sheet, err := broker.Create(t.Context(), Request{URL: carrier.URL})
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	RegisterRoutes(e.Group("/api/v1"), broker)
	req := httptest.NewRequest(http.MethodGet, sheet.proxyURL(carrier.URL)+"?token=retired-secret", nil)
	req.Header.Set("Origin", "null")
	for _, name := range []string{"Authorization", "Cookie", "Cf-Access-Jwt-Assertion", "Cf-Access-Client-Secret"} {
		req.Header.Set(name, "private")
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "null" || rec.Header().Get("Referrer-Policy") != "no-referrer" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox allow-scripts allow-forms;") {
		t.Fatalf("carrier isolation headers = %v", rec.Header())
	}
	if strings.Contains(rec.Body.String(), "retired-secret") {
		t.Fatal("general credential leaked into carrier HTML")
	}
	for _, suffix := range []string{"/proxy", "/proxy/resource", "/callback"} {
		t.Run("preflight"+suffix, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, sheet.Info().EmbedURL+suffix, nil)
			req.Header.Set("Origin", "null")
			req.Header.Set("Access-Control-Request-Method", http.MethodPost)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 || rec.Header().Get("Access-Control-Allow-Origin") != "null" {
				t.Fatalf("preflight = %d, %s, %v", rec.Code, rec.Body, rec.Header())
			}
		})
	}
	policy = "changed"
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, sheet.Info().EmbedURL, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("changed policy status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, sheet.Info().EmbedURL+"/callback", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("revoked preflight status = %d", rec.Code)
	}
}
