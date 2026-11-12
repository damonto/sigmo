package settings

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/internet"
	appsettings "github.com/damonto/sigmo/internal/pkg/settings"
)

// ProxyHandler manages the saved proxy configuration and its runtime application.
type ProxyHandler struct {
	store             *appsettings.Store
	internetConnector *internet.Connector
}

// NewProxyHandler creates proxy configuration endpoints.
func NewProxyHandler(store *appsettings.Store, connector *internet.Connector) *ProxyHandler {
	return &ProxyHandler{store: store, internetConnector: connector}
}

func (h *ProxyHandler) UpdateProxy(c *echo.Context) error {
	var req ProxyValues
	if err := httpapi.BindJSON(c, &req, "update_proxy_invalid_request"); err != nil {
		return err
	}
	req = normalizeProxyValues(req)
	if err := c.Validate(&req); err != nil {
		return httpapi.UnprocessableEntity(c, "update_proxy_invalid", err)
	}

	proxy := proxySettingsFromValues(req)
	saved, err := h.store.Update(c.Request().Context(), func(current *appsettings.Settings) error {
		current.Proxy = proxy
		return nil
	})
	if err != nil {
		return httpapi.Internal(c, "update_proxy_failed", fmt.Errorf("save proxy settings: %w", err))
	}

	if err := h.internetConnector.UpdateProxyConfig(internetProxyConfig(saved)); err != nil {
		return httpapi.Internal(c, "reload_proxy_settings_failed", fmt.Errorf("saved proxy settings, reload proxy: %w", err))
	}

	return c.JSON(http.StatusOK, responseFromSettings(saved))
}
