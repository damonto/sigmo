package mcpadmin

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/app/mcpauth"
	"github.com/damonto/sigmo/internal/app/mcpserver"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type Config struct {
	Settings   *settings.Store
	Keys       *mcpauth.Store
	Storage    *storage.Store
	Controller *mcpserver.Controller
}

type Handler struct {
	updateMu   sync.Mutex
	settings   *settings.Store
	keys       *mcpauth.Store
	storage    *storage.Store
	controller *mcpserver.Controller
}

func New(cfg Config) *Handler {
	return &Handler{settings: cfg.Settings, keys: cfg.Keys, storage: cfg.Storage, controller: cfg.Controller}
}

type settingsResponse struct {
	Enabled            bool                   `json:"enabled"`
	EndpointPath       string                 `json:"endpointPath"`
	AuditRetentionDays int                    `json:"auditRetentionDays"`
	Permissions        []mcpserver.Permission `json:"permissions"`
}

type updateSettingsRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *Handler) GetSettings(c *echo.Context) error {
	return c.JSON(http.StatusOK, h.settingsResponse())
}

func (h *Handler) UpdateSettings(c *echo.Context) error {
	var req updateSettingsRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, "update_mcp_settings_invalid_request", err)
	}
	h.updateMu.Lock()
	defer h.updateMu.Unlock()
	_, err := h.settings.Update(c.Request().Context(), func(current *settings.Settings) error {
		current.MCP.Enabled = req.Enabled
		return nil
	})
	if err != nil {
		return httpapi.Internal(c, "update_mcp_settings_failed", fmt.Errorf("save MCP settings: %w", err))
	}
	h.controller.SetEnabled(req.Enabled)
	return c.JSON(http.StatusOK, h.settingsResponse())
}

func (h *Handler) settingsResponse() settingsResponse {
	return settingsResponse{
		Enabled:            h.controller.Enabled(),
		EndpointPath:       mcpserver.EndpointPath,
		AuditRetentionDays: int(mcpserver.AuditRetention / (24 * time.Hour)),
		Permissions:        h.controller.Catalog().Permissions(),
	}
}

type apiKeyResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenHint   string     `json:"tokenHint"`
	Status      string     `json:"status"`
	AllModems   bool       `json:"allModems"`
	ModemIDs    []string   `json:"modemIds"`
	Permissions []string   `json:"permissions"`
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty"`
}

type apiKeysResponse struct {
	APIKeys []apiKeyResponse `json:"apiKeys"`
}

type createAPIKeyRequest struct {
	Name         string   `json:"name"`
	ValidityDays *int     `json:"validityDays"`
	AllModems    bool     `json:"allModems"`
	ModemIDs     []string `json:"modemIds"`
	Permissions  []string `json:"permissions"`
}

type createAPIKeyResponse struct {
	APIKey apiKeyResponse `json:"apiKey"`
	Token  string         `json:"token"`
}

func (h *Handler) ListAPIKeys(c *echo.Context) error {
	grants, err := h.keys.List(c.Request().Context())
	if err != nil {
		return httpapi.Internal(c, "list_mcp_api_keys_failed", err)
	}
	values := make([]apiKeyResponse, 0, len(grants))
	for _, grant := range grants {
		values = append(values, apiKeyFromGrant(grant))
	}
	return c.JSON(http.StatusOK, apiKeysResponse{APIKeys: values})
}

func (h *Handler) CreateAPIKey(c *echo.Context) error {
	var req createAPIKeyRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, "create_mcp_api_key_invalid_request", err)
	}
	for _, permission := range req.Permissions {
		if !h.controller.Catalog().SupportsPermission(strings.TrimSpace(permission)) {
			return httpapi.UnprocessableEntity(c, "create_mcp_api_key_invalid_permission", fmt.Errorf("MCP permission %q is unavailable", permission))
		}
	}
	validityDays := mcpauth.DefaultValidityDays
	if req.ValidityDays != nil {
		validityDays = *req.ValidityDays
	}
	grant, token, err := h.keys.Issue(c.Request().Context(), mcpauth.IssueRequest{
		Name: req.Name, ValidityDays: validityDays, AllModems: req.AllModems, ModemIDs: req.ModemIDs, Permissions: req.Permissions,
	})
	if err != nil {
		return httpapi.UnprocessableEntity(c, "create_mcp_api_key_invalid", err)
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusCreated, createAPIKeyResponse{APIKey: apiKeyFromGrant(grant), Token: token})
}

func (h *Handler) RevokeAPIKey(c *echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return httpapi.BadRequest(c, "revoke_mcp_api_key_invalid_request", errors.New("API key id is required"))
	}
	_, err := h.keys.Revoke(c.Request().Context(), id)
	if err != nil {
		return httpapi.Internal(c, "revoke_mcp_api_key_failed", err)
	}
	h.controller.CloseKey(id)
	return c.NoContent(http.StatusNoContent)
}

func apiKeyFromGrant(grant mcpauth.Grant) apiKeyResponse {
	return apiKeyResponse{
		ID: grant.ID, Name: grant.Name, TokenHint: grant.TokenHint, Status: grant.Status(time.Now()), AllModems: grant.AllModems,
		ModemIDs: grant.ModemIDs, Permissions: grant.Permissions, CreatedAt: grant.CreatedAt, ExpiresAt: grant.ExpiresAt, RevokedAt: grant.RevokedAt,
	}
}

type auditEventResponse struct {
	ID         int64     `json:"id"`
	KeyID      string    `json:"keyId"`
	KeyName    string    `json:"keyName"`
	Tool       string    `json:"tool"`
	ModemIDs   []string  `json:"modemIds"`
	Outcome    string    `json:"outcome"`
	ErrorCode  string    `json:"errorCode,omitempty"`
	DurationMS int64     `json:"durationMs"`
	CreatedAt  time.Time `json:"createdAt"`
}

type auditEventsResponse struct {
	Events     []auditEventResponse `json:"events"`
	NextCursor int64                `json:"nextCursor,omitempty"`
}

func (h *Handler) ListAuditEvents(c *echo.Context) error {
	before, err := resolveInteger(c.QueryParam("before"), 0)
	if err != nil {
		return httpapi.BadRequest(c, "list_mcp_audit_events_invalid_cursor", err)
	}
	limit, err := resolveInteger(c.QueryParam("limit"), 50)
	if err != nil {
		return httpapi.BadRequest(c, "list_mcp_audit_events_invalid_limit", err)
	}
	if limit == 0 {
		limit = 50
	}
	limit = min(limit, 100)
	events, err := h.storage.ListMCPAuditEvents(c.Request().Context(), before, int(limit))
	if err != nil {
		return httpapi.Internal(c, "list_mcp_audit_events_failed", err)
	}
	response := auditEventsResponse{Events: make([]auditEventResponse, 0, len(events))}
	for _, event := range events {
		response.Events = append(response.Events, auditEventResponse{
			ID: event.ID, KeyID: event.KeyID, KeyName: event.KeyName, Tool: event.Tool, ModemIDs: event.ModemIDs,
			Outcome: event.Outcome, ErrorCode: event.ErrorCode, DurationMS: event.Duration.Milliseconds(), CreatedAt: event.CreatedAt,
		})
	}
	if len(events) == int(limit) && len(events) > 0 {
		response.NextCursor = events[len(events)-1].ID
	}
	return c.JSON(http.StatusOK, response)
}

func resolveInteger(raw string, fallback int64) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid integer %q", raw)
	}
	return value, nil
}

func (h *Handler) DownloadSkill(c *echo.Context) error {
	archive, err := mcpserver.SkillArchive()
	if err != nil {
		return httpapi.Internal(c, "download_mcp_skill_failed", err)
	}
	response := c.Response()
	response.Header().Set("Content-Type", "application/zip")
	response.Header().Set("Content-Disposition", `attachment; filename="sigmo-control.zip"`)
	return c.Blob(http.StatusOK, "application/zip", archive)
}
