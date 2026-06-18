//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

	coreesim "github.com/damonto/sigmo/internal/app/handler/esim"
	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	registry *mmodem.Registry
	service  *Service
}

const (
	errorCodeListTransferSourcesFailed  = "list_transfer_sources_failed"
	errorCodeListTransferProfilesFailed = "list_transfer_profiles_failed"
	errorCodeTransferInvalidRequest     = "transfer_invalid_request"
	errorCodeTransferSourceIMEIRequired = "transfer_source_imei_required"
	errorCodeTransferSourceNotFound     = "transfer_source_not_found"
	errorCodeTransferSourceUnsupported  = "transfer_source_unsupported"
	errorCodeTransferESIMFailed         = "transfer_esim_failed"
)

var transferWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewHandler(cfg Config) *Handler {
	return &Handler{
		registry: cfg.Registry,
		service:  New(cfg),
	}
}

func RegisterRoutes(group *echo.Group, cfg Config) {
	h := NewHandler(cfg)
	group.GET("/modems/:id/esim-transfer-sources", h.Sources)
	group.POST("/modems/:id/esim-transfer-profile-queries", h.Profiles)
	group.GET("/modems/:id/esim-transfer-sessions", h.Transfer)
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
		return httpapi.ModemLookupError(c, err, errorCodeListTransferSourcesFailed)
	}
	response, err := h.service.Sources(ctx, target)
	if err != nil {
		return httpapi.Internal(c, errorCodeListTransferSourcesFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Profiles(c *echo.Context) error {
	ctx := c.Request().Context()
	target, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListTransferProfilesFailed)
	}
	var req ProfilesRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeTransferInvalidRequest, err)
	}
	profiles, err := h.service.Profiles(ctx, target, req)
	if err != nil {
		return transferProfileError(c, err)
	}
	return c.JSON(http.StatusOK, profiles)
}

func (h *Handler) Transfer(c *echo.Context) error {
	ctx := c.Request().Context()
	target, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeTransferESIMFailed)
	}
	conn, err := transferWSUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	return h.service.Serve(ctx, conn, target)
}

func transferProfileError(c *echo.Context, err error) error {
	if errors.Is(err, ErrSourceIMEIRequired) {
		return httpapi.BadRequest(c, errorCodeTransferSourceIMEIRequired, err)
	}
	if errors.Is(err, mmodem.ErrNotFound) {
		return httpapi.NotFound(c, errorCodeTransferSourceNotFound, err)
	}
	if errors.Is(err, ErrSourceUnsupported) {
		return httpapi.BadRequest(c, errorCodeTransferSourceUnsupported, err)
	}
	if errors.Is(err, ErrSourceIsTarget) {
		return httpapi.BadRequest(c, errorCodeTransferInvalidRequest, err)
	}
	return httpapi.Internal(c, errorCodeListTransferProfilesFailed, err)
}

func profileActionConfig(core *coreesim.Handler, cfg Config) (Config, error) {
	if core == nil {
		return Config{}, errors.New("eSIM handler is required")
	}
	cfg.EnableProfile = func(ctx context.Context, modem *mmodem.Modem, iccid sgp22.ICCID) error {
		if err := core.EnableProfile(ctx, modem, iccid); err != nil {
			return fmt.Errorf("enable profile: %w", err)
		}
		return nil
	}
	cfg.DeleteProfile = func(ctx context.Context, modem *mmodem.Modem, iccid sgp22.ICCID) error {
		if err := core.DeleteProfile(ctx, modem, iccid); err != nil {
			return fmt.Errorf("delete profile: %w", err)
		}
		return nil
	}
	return cfg, nil
}
