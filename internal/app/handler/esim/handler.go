package esim

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/labstack/echo/v5"

	appesim "github.com/damonto/sigmo/internal/app/esim"
	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// Handler adapts profile operations to HTTP.
type Handler struct {
	registry *mmodem.Registry
	service  *appesim.Service
}

// New creates profile HTTP handlers using the shared application service.
func New(registry *mmodem.Registry, service *appesim.Service) *Handler {
	return &Handler{registry: registry, service: service}
}

var (
	errICCIDRequired = errors.New("iccid is required")
	errInvalidICCID  = errors.New("invalid iccid")
	errEuiccBusy     = errors.New("eUICC is busy, retry later")
)

var errEnableTimeout = errors.New("enabling timed out, please refresh to confirm whether the profile is active")

func (h *Handler) List(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "list_esims_failed")
	}
	response, err := h.service.ProfileInventory(c.Request().Context(), modem)
	if err != nil {
		return httpapi.LPAError(c, err, "list_esims_failed")
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Discovery(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "discover_esims_failed")
	}
	response, err := h.service.Discover(ctx, modem, c.Param("seId"))
	if err != nil {
		return httpapi.LPAError(c, err, "discover_esims_failed")
	}
	return c.JSON(http.StatusOK, response)
}

// SetActiveProfile replaces the active profile selection for a secure element.
func (h *Handler) SetActiveProfile(c *echo.Context) error {
	ctx := c.Request().Context()

	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "enable_esim_failed")
	}
	var req struct {
		ICCID string `json:"iccid"`
	}
	if err := httpapi.BindJSON(c, &req, "invalid_iccid"); err != nil {
		return err
	}
	iccid, err := parseICCID(req.ICCID)
	if err != nil {
		if errors.Is(err, errICCIDRequired) {
			return httpapi.BadRequest(c, "iccid_required", err)
		}
		return httpapi.BadRequest(c, "invalid_iccid", err)
	}
	if err := h.service.EnableProfile(ctx, modem, c.Param("seId"), iccid); err != nil {
		return enablePrepareError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

func enablePrepareError(c *echo.Context, err error) error {
	if errors.Is(err, appesim.ErrProfileAlreadyActive) {
		return c.NoContent(http.StatusNoContent)
	}
	return enableError(c, err)
}

func enableError(c *echo.Context, err error) error {
	if errors.Is(err, mmodem.ErrNotFound) {
		return httpapi.ModemLookupError(c, err, "enable_esim_failed")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return httpapi.RequestTimeout(c, "esim_enable_timeout", errEnableTimeout)
	}
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if errors.Is(err, sgp22.ErrCatBusy) {
		return httpapi.Error(c, http.StatusConflict, "esim_enable_busy", errEuiccBusy.Error())
	}
	if errors.Is(err, appesim.ErrProfileNotFound) {
		return httpapi.BadRequest(c, "esim_profile_not_found", err)
	}
	return httpapi.LPAError(c, err, "enable_esim_failed")
}

func (h *Handler) Delete(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "delete_esim_failed")
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		if errors.Is(err, errICCIDRequired) {
			return httpapi.BadRequest(c, "iccid_required", err)
		}
		return httpapi.BadRequest(c, "invalid_iccid", err)
	}
	if err := h.service.DeleteProfile(c.Request().Context(), modem, c.Param("seId"), iccid); err != nil {
		if errors.Is(err, appesim.ErrActiveProfileCannotDelete) {
			return httpapi.Error(c, http.StatusConflict, "active_esim_profile", err.Error())
		}
		if errors.Is(err, appesim.ErrProfileNotFound) {
			return httpapi.NotFound(c, "esim_profile_not_found", err)
		}
		return httpapi.LPAError(c, err, "delete_esim_failed")
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) UpdateNickname(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_esim_nickname_failed")
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		if errors.Is(err, errICCIDRequired) {
			return httpapi.BadRequest(c, "iccid_required", err)
		}
		return httpapi.BadRequest(c, "invalid_iccid", err)
	}
	var req UpdateNicknameRequest
	if err := httpapi.BindAndValidate(c, &req, "update_esim_nickname_invalid_request"); err != nil {
		return err
	}
	if err := h.service.UpdateNickname(c.Request().Context(), modem, c.Param("seId"), iccid, req.Nickname); err != nil {
		if errors.Is(err, appesim.ErrInvalidNickname) {
			return httpapi.BadRequest(c, "invalid_nickname", err)
		}
		return httpapi.LPAError(c, err, "update_esim_nickname_failed")
	}
	return c.NoContent(http.StatusNoContent)
}

func iccidFromParam(c *echo.Context) (sgp22.ICCID, error) {
	return parseICCID(c.Param("iccid"))
}

func parseICCID(iccidParam string) (sgp22.ICCID, error) {
	if iccidParam == "" {
		return nil, errICCIDRequired
	}
	iccid, err := sgp22.NewICCID(iccidParam)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", errInvalidICCID, iccidParam, err)
	}
	return iccid, nil
}
