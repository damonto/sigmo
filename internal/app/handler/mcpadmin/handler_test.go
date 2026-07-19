package mcpadmin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/mcpauth"
	"github.com/damonto/sigmo/internal/app/mcpserver"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestAPIKeyLifecycle(t *testing.T) {
	h, keys, _ := newTestHandler(t)
	e := echo.New()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/mcp/api-keys", strings.NewReader(`{
		"name":"Agent","allModems":false,"modemIds":["imei-b","imei-a"],"permissions":["modem.read"]
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if err := h.CreateAPIKey(e.NewContext(req, rec)); err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("CreateAPIKey() status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var created createAPIKeyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal(create response) error = %v", err)
	}
	if created.Token == "" || created.APIKey.Name != "Agent" || created.APIKey.AllModems {
		t.Fatalf("create response = %+v", created)
	}
	if got, want := created.APIKey.ModemIDs, []string{"imei-a", "imei-b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("created modem IDs = %v, want %v", got, want)
	}
	if strings.Contains(rec.Body.String(), "tokenHash") {
		t.Fatalf("create response exposes token hash: %s", rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings/mcp/api-keys", nil)
	listRec := httptest.NewRecorder()
	if err := h.ListAPIKeys(e.NewContext(listReq, listRec)); err != nil {
		t.Fatalf("ListAPIKeys() error = %v", err)
	}
	if strings.Contains(listRec.Body.String(), created.Token) || strings.Contains(listRec.Body.String(), "tokenHash") {
		t.Fatalf("list response exposes secret material: %s", listRec.Body.String())
	}

	revokeReq := httptest.NewRequest(http.MethodDelete, "/api/v1/settings/mcp/api-keys/"+created.APIKey.ID, nil)
	revokeRec := httptest.NewRecorder()
	c := e.NewContext(revokeReq, revokeRec)
	c.SetPathValues(echo.PathValues{{Name: "id", Value: created.APIKey.ID}})
	if err := h.RevokeAPIKey(c); err != nil {
		t.Fatalf("RevokeAPIKey() error = %v", err)
	}
	if revokeRec.Code != http.StatusNoContent {
		t.Fatalf("RevokeAPIKey() status = %d, want %d", revokeRec.Code, http.StatusNoContent)
	}
	if _, err := keys.Validate(context.Background(), created.Token); !errors.Is(err, mcpauth.ErrRevoked) {
		t.Fatalf("Validate(revoked) error = %v, want ErrRevoked", err)
	}
}

func TestCreateAPIKeyRejectsUnavailablePermission(t *testing.T) {
	h, _, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/mcp/api-keys", strings.NewReader(`{
		"name":"Agent","allModems":true,"permissions":["unsupported.permission"]
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if err := h.CreateAPIKey(echo.New().NewContext(req, rec)); err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("CreateAPIKey() status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateAPIKeyValidityDays(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		wantCode int
		wantDays int
	}{
		{name: "omitted uses default", wantCode: http.StatusCreated, wantDays: mcpauth.DefaultValidityDays},
		{name: "zero is rejected", field: `,"validityDays":0`, wantCode: http.StatusUnprocessableEntity},
		{name: "minimum", field: `,"validityDays":1`, wantCode: http.StatusCreated, wantDays: 1},
		{name: "maximum", field: `,"validityDays":180`, wantCode: http.StatusCreated, wantDays: 180},
		{name: "above maximum", field: `,"validityDays":181`, wantCode: http.StatusUnprocessableEntity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _, _ := newTestHandler(t)
			body := `{"name":"Agent","allModems":true,"permissions":["modem.read"]` + tt.field + `}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/mcp/api-keys", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			if err := h.CreateAPIKey(echo.New().NewContext(req, rec)); err != nil {
				t.Fatalf("CreateAPIKey() error = %v", err)
			}
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantCode, rec.Body.String())
			}
			if tt.wantCode != http.StatusCreated {
				return
			}
			var created createAPIKeyResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if got := created.APIKey.ExpiresAt.Sub(created.APIKey.CreatedAt); got != time.Duration(tt.wantDays)*24*time.Hour {
				t.Fatalf("validity = %v, want %d days", got, tt.wantDays)
			}
		})
	}
}

func TestConcurrentSettingsUpdatesRemainConsistent(t *testing.T) {
	h, _, controller := newTestHandler(t)
	e := echo.New()

	for range 50 {
		var wg sync.WaitGroup
		for _, enabled := range []bool{true, false} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				body := `{"enabled":false}`
				if enabled {
					body = `{"enabled":true}`
				}
				req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/mcp", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				if err := h.UpdateSettings(e.NewContext(req, rec)); err != nil {
					t.Errorf("UpdateSettings() error = %v", err)
				}
			}()
		}
		wg.Wait()
		if stored := h.settings.MCPSettings().Enabled; stored != controller.Enabled() {
			t.Fatalf("stored enabled = %v, runtime enabled = %v", stored, controller.Enabled())
		}
	}
}

func TestSettingsAndSkillDownload(t *testing.T) {
	h, _, controller := newTestHandler(t)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/mcp", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if err := h.UpdateSettings(e.NewContext(req, rec)); err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if rec.Code != http.StatusOK || !controller.Enabled() {
		t.Fatalf("UpdateSettings() status = %d, enabled = %v", rec.Code, controller.Enabled())
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings/mcp/skills/sigmo-control", nil)
	downloadRec := httptest.NewRecorder()
	if err := h.DownloadSkill(e.NewContext(downloadReq, downloadRec)); err != nil {
		t.Fatalf("DownloadSkill() error = %v", err)
	}
	if downloadRec.Code != http.StatusOK || downloadRec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("DownloadSkill() status = %d, content type = %q", downloadRec.Code, downloadRec.Header().Get("Content-Type"))
	}
	if _, err := zip.NewReader(bytes.NewReader(downloadRec.Body.Bytes()), int64(downloadRec.Body.Len())); err != nil {
		t.Fatalf("downloaded Skill ZIP error = %v", err)
	}
}

func newTestHandler(t *testing.T) (*Handler, *mcpauth.Store, *mcpserver.Controller) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	store, err := settings.NewStore(ctx, db)
	if err != nil {
		t.Fatalf("settings.NewStore() error = %v", err)
	}
	keys, err := mcpauth.NewStore(db)
	if err != nil {
		t.Fatalf("mcpauth.NewStore() error = %v", err)
	}
	catalog := mcpserver.NewCatalog()
	if err := catalog.AddPermission("modem.read", "modem"); err != nil {
		t.Fatalf("AddPermission() error = %v", err)
	}
	controller, err := mcpserver.New(mcpserver.Config{BuildVersion: "test", Settings: store, Keys: keys, Storage: db, Catalog: catalog})
	if err != nil {
		t.Fatalf("mcpserver.New() error = %v", err)
	}
	t.Cleanup(controller.Close)
	h := New(Config{Settings: store, Keys: keys, Storage: db, Controller: controller})
	return h, keys, controller
}
