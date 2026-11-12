package esimtransfer

import (
	"errors"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

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

// NewHandler adapts the shared transfer service to HTTP and WebSocket clients.
func NewHandler(service *Service) *Handler {
	return &Handler{registry: service.registry, runner: service.runner, peers: service.peers}
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
	if err := httpapi.BindJSON(c, &req, "transfer_invalid_request"); err != nil {
		return err
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
