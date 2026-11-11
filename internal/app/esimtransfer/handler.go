package esimtransfer

import (
	"context"
	"errors"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

	coreesim "github.com/damonto/sigmo/internal/app/handler/esim"
	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	registry *mmodem.Registry
	runner   *transferRunner
	peers    *peerBroker
}

var transferWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewHandler(cfg Config) *Handler {
	broker := newPeerBroker(cfg.Allowed)
	runner := newTransferRunner(cfg)
	return &Handler{registry: cfg.Registry, runner: runner, peers: broker}
}

func RegisterRoutes(group *echo.Group, cfg Config) {
	h := NewHandler(cfg)
	h.registerRoutes(group)
}

// RegisterWithPeerRoutes keeps remote grants outside UI login while preserving
// the access and feature middleware supplied for each route group.
func RegisterWithPeerRoutes(group, resources *echo.Group, cfg Config) *Handler {
	h := NewHandler(cfg)
	h.registerRoutes(group)
	RegisterPeerRoutes(resources, h)
	return h
}

func (h *Handler) registerRoutes(group *echo.Group) {
	group.GET("/modems/:id/esim-transfers/sources", h.Sources)
	group.POST("/modems/:id/esim-transfers/source-profiles", h.Profiles)
	group.GET("/modems/:id/esim-transfers/sessions", h.Transfer)
	h.RegisterPeerControlRoutes(group)
}

// RegisterPeerRoutes registers capability-scoped server connections separately
// from UI login. The caller must retain its existing entry access boundary.
func RegisterPeerRoutes(group *echo.Group, h *Handler) {
	group.GET("/esim-transfers/remote-sessions", h.RemoteSession)
}

func ConfigFromCore(core *coreesim.Handler, cfg Config) Config {
	cfg.EnableProfile = core.EnableProfile
	cfg.DeleteProfile = core.DeleteProfile
	return cfg
}

func (h *Handler) Sources(c *echo.Context) error {
	ctx := c.Request().Context()
	target, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "list_transfer_sources_failed")
	}
	response, err := h.runner.Sources(ctx, target)
	if err != nil {
		return httpapi.Internal(c, "list_transfer_sources_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Profiles(c *echo.Context) error {
	ctx := c.Request().Context()
	target, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "list_transfer_profiles_failed")
	}
	var req ProfilesRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, "transfer_invalid_request", err)
	}
	profiles, err := h.runner.Profiles(ctx, target, req)
	if err != nil {
		return transferProfileError(c, err)
	}
	return c.JSON(http.StatusOK, profiles)
}

func (h *Handler) Transfer(c *echo.Context) error {
	ctx, release, err := h.peers.begin(c.Request().Context())
	if err != nil {
		return httpapi.Error(c, http.StatusServiceUnavailable, "transfer_esim_failed", err.Error())
	}
	defer release()
	target, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "transfer_esim_failed")
	}
	conn, err := transferWSUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	return h.runner.Serve(ctx, conn, target)
}

func transferProfileError(c *echo.Context, err error) error {
	if errors.Is(err, ErrSourceIMEIRequired) {
		return httpapi.BadRequest(c, "transfer_source_imei_required", err)
	}
	if errors.Is(err, mmodem.ErrNotFound) {
		return httpapi.NotFound(c, "transfer_source_not_found", err)
	}
	if errors.Is(err, ErrSourceUnsupported) {
		return httpapi.BadRequest(c, "transfer_source_unsupported", err)
	}
	if errors.Is(err, ErrSourceIsTarget) {
		return httpapi.BadRequest(c, "transfer_invalid_request", err)
	}
	return httpapi.Internal(c, "list_transfer_profiles_failed", err)
}

// Close cancels remote transfers and subscriptions before releasing device pools.
func (h *Handler) Close(ctx context.Context) error { return h.peers.close(ctx) }
