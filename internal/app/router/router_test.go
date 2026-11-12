package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/call"
	"github.com/damonto/sigmo/internal/app/esimtransfer"
	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/damonto/sigmo/internal/app/httpapi"
	appmodem "github.com/damonto/sigmo/internal/app/modem"
	"github.com/damonto/sigmo/internal/app/network"
	"github.com/damonto/sigmo/internal/app/websheet"
	"github.com/damonto/sigmo/internal/pkg/license"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/networkprefs"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestRouteAccessBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name            string
		mode            string
		authorized      bool
		protectedStatus int
		licensedStatus  int
		websheetStatus  int
		remoteStatus    int
		preflightStatus int
	}{
		{"base features without Pro", "none", false, http.StatusOK, http.StatusForbidden, http.StatusForbidden, http.StatusForbidden, http.StatusForbidden},
		{"capability URLs without UI login", "otp", true, http.StatusUnauthorized, http.StatusUnauthorized, http.StatusFound, http.StatusBadRequest, http.StatusNoContent},
		{"Access protects capability URLs", "cloudflare_access", true, http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized, http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestConfig(t)
			current := settings.Default()
			current.Auth.Mode = tt.mode
			deps.Store = settings.NewMemoryStore(current)
			deps.License = &license.Controller{}
			deps.Policy = feature.NewPolicy(func() bool { return tt.authorized })
			broker := websheet.New(websheet.Config{})
			sheet, err := broker.Create(t.Context(), websheet.Request{URL: "https://example.com/portal", LookupNetIP: func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("192.0.2.1")}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			transfer := esimtransfer.New(esimtransfer.Config{
				Store:   deps.Store,
				Allowed: func() bool { return deps.Policy.Allows(feature.ESIMTransfer) },
			})
			t.Cleanup(func() {
				if err := transfer.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			deps.Extensions = []Extension{
				WebsheetRoutes(broker),
				IMSRoutes(nil),
				CallRoutes(call.New(deps.Storage, nil), nil),
				ESIMTransferRoutes(transfer),
			}
			e := echo.New()
			e.HTTPErrorHandler = httpapi.HTTPErrorHandler
			if err := Register(e, deps); err != nil {
				t.Fatal(err)
			}
			for _, request := range []struct {
				method string
				path   string
				status int
			}{
				{http.MethodGet, "/api/v1/contacts", tt.protectedStatus},
				{http.MethodGet, "/api/v1/calls", tt.protectedStatus},
				{http.MethodGet, "/api/v1/license", tt.protectedStatus},
				{http.MethodGet, "/api/v1/modems/test/wifi-calling/settings", tt.licensedStatus},
				{http.MethodGet, "/api/v1/modems/test/volte/settings", tt.licensedStatus},
				{http.MethodGet, "/api/v1/modems/test/esim-transfers/sources", tt.licensedStatus},
				{http.MethodGet, "/api/v1/esim-transfer-requests/events", tt.licensedStatus},
				{http.MethodPost, "/api/v1/esim-transfer-requests/test/decisions", tt.licensedStatus},
				{http.MethodGet, "/api/v1/esim-transfers/remote-sessions", tt.remoteStatus},
				{http.MethodGet, sheet.Info().EmbedURL, tt.websheetStatus},
				{http.MethodOptions, sheet.Info().EmbedURL + "/callback", tt.preflightStatus},
			} {
				t.Run(request.method+" "+request.path, func(t *testing.T) {
					req := httptest.NewRequest(request.method, request.path, nil)
					if request.method == http.MethodOptions {
						req.Header.Set("Origin", "null")
						req.Header.Set("Access-Control-Request-Method", http.MethodPost)
					}
					rec := httptest.NewRecorder()
					e.ServeHTTP(rec, req)
					if rec.Code != request.status {
						t.Fatalf("status = %d, want %d: %s", rec.Code, request.status, rec.Body.String())
					}
				})
			}
		})
	}
}

func newTestConfig(t *testing.T) RegisterConfig {
	t.Helper()
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "router.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	preferences, err := networkprefs.New(db)
	if err != nil {
		t.Fatal(err)
	}
	networkHandler, err := network.New(network.Config{Registry: &modem.Registry{}, Preferences: preferences, Store: db})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(networkHandler.Close)
	store := settings.NewMemoryStore(settings.Default())
	return RegisterConfig{
		Modems:  appmodem.New(appmodem.Config{Store: store, Storage: db}),
		Store:   store,
		Storage: db,
		Network: networkHandler,
	}
}

func TestRESTResourceBindingsKeepAuthentication(t *testing.T) {
	deps := newTestConfig(t)
	current := settings.Default()
	current.Auth.Mode = "otp"
	deps.Store = settings.NewMemoryStore(current)
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.Response().Header().Set("X-Test-Route", c.Path())
			return next(c)
		}
	})
	if err := Register(e, deps); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []struct {
		method, path, pattern string
	}{
		{http.MethodPut, "/sim-slot-selection", "/sim-slot-selection"},
		{http.MethodGet, "/network-settings/modes", "/network-settings/modes"},
		{http.MethodPut, "/network-settings/modes", "/network-settings/modes"},
		{http.MethodGet, "/network-settings/bands", "/network-settings/bands"},
		{http.MethodPut, "/network-settings/bands", "/network-settings/bands"},
		{http.MethodGet, "/secure-elements", "/secure-elements"},
		{http.MethodGet, "/esim-profiles", "/esim-profiles"},
		{http.MethodPut, "/secure-elements/se0/active-esim-profile", "/secure-elements/:seId/active-esim-profile"},
		{http.MethodGet, "/message-conversations", "/message-conversations"},
		{http.MethodGet, "/message-conversations/person/messages", "/message-conversations/:participant/messages"},
		{http.MethodDelete, "/message-conversations/person", "/message-conversations/:participant"},
	} {
		t.Run(resource.method+" "+resource.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(resource.method, "/api/v1/modems/test"+resource.path, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want authentication challenge: %s", rec.Code, rec.Body.String())
			}
			if got, want := rec.Header().Get("X-Test-Route"), "/api/v1/modems/:id"+resource.pattern; got != want {
				t.Fatalf("matched route = %q, want %q", got, want)
			}
		})
	}
}

func TestReplacedRESTPathsHaveNoCompatibilityAliases(t *testing.T) {
	e := echo.New()
	if err := Register(e, newTestConfig(t)); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []struct{ method, path string }{
		{http.MethodPut, "/sim-slots/2"},
		{http.MethodGet, "/networks/airplane-mode"},
		{http.MethodPut, "/networks/current-modes"},
		{http.MethodGet, "/ses"},
		{http.MethodGet, "/esims"},
		{http.MethodGet, "/messages"},
	} {
		t.Run(resource.method+" "+resource.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(resource.method, "/api/v1/modems/test"+resource.path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("removed route status = %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}
