package update

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	appupdate "github.com/damonto/sigmo/internal/app/update"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type Handler struct {
	controller *appupdate.Controller
}

type UpdateSettingsRequest struct {
	Automatic bool   `json:"automatic"`
	Channel   string `json:"channel" validate:"required,oneof=stable dev"`
}

func New(controller *appupdate.Controller) *Handler {
	return &Handler{controller: controller}
}

func (h *Handler) GetSettings(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, h.controller.Snapshot())
}

func (h *Handler) UpdateSettings(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	var req UpdateSettingsRequest
	if err := httpapi.BindAndValidate(c, &req, "update_settings_invalid"); err != nil {
		return err
	}
	snapshot, err := h.controller.UpdateSettings(c.Request().Context(), settings.Updates{
		Automatic: req.Automatic,
		Channel:   req.Channel,
	})
	if err != nil {
		if errors.Is(err, appupdate.ErrBusy) {
			return httpapi.Error(c, http.StatusConflict, "update_busy", err.Error())
		}
		return httpapi.UnprocessableEntity(c, "update_settings_failed", err)
	}
	return c.JSON(http.StatusOK, snapshot)
}

func (h *Handler) CreateCheck(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	if err := h.controller.Check(c.Request().Context()); err != nil {
		if errors.Is(err, appupdate.ErrBusy) {
			return httpapi.Error(c, http.StatusConflict, "update_busy", err.Error())
		}
		code := appupdate.ErrorCode(err)
		if code == "" {
			code = "update_check_failed"
		}
		return httpapi.Error(c, http.StatusBadGateway, code, err.Error())
	}
	return c.JSON(http.StatusOK, h.controller.Snapshot())
}

func (h *Handler) CreateInstallation(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	if err := h.controller.StartInstall(); err != nil {
		if errors.Is(err, appupdate.ErrBusy) {
			return httpapi.Error(c, http.StatusConflict, "update_busy", err.Error())
		}
		if errors.Is(err, appupdate.ErrNoUpdate) {
			return httpapi.Error(c, http.StatusConflict, "update_unavailable", err.Error())
		}
		if errors.Is(err, appupdate.ErrSelfUpdateUnsupported) {
			return httpapi.Error(c, http.StatusConflict, "self_update_unsupported", err.Error())
		}
		return httpapi.Internal(c, "start_update_failed", err)
	}
	return c.JSON(http.StatusAccepted, h.controller.Snapshot())
}

func (h *Handler) CurrentInstallation(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, h.controller.Snapshot())
}
