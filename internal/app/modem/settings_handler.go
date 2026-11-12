package modem

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// SettingsHandler manages per-modem preferences.
type SettingsHandler struct {
	registry *mmodem.Registry
	settings *modemSettings
}

// NewSettingsHandler creates modem preference endpoints.
func NewSettingsHandler(service *Service) *SettingsHandler {
	return &SettingsHandler{registry: service.registry, settings: service.settings}
}

func (h *SettingsHandler) UpdateSettings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_settings_failed")
	}
	var req UpdateModemSettingsRequest
	if err := httpapi.BindAndValidate(c, &req, "update_settings_invalid_request"); err != nil {
		return err
	}
	if err := h.settings.Update(c.Request().Context(), modem, req); err != nil {
		return httpapi.Internal(c, "update_settings_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *SettingsHandler) Settings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "get_settings_failed")
	}
	response := h.settings.Get(modem)
	return c.JSON(http.StatusOK, response)
}
