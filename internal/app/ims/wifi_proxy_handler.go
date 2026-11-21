package ims

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
)

// ListWiFiProxies returns the configured SOCKS5 proxy metadata.
func (h *Handler) ListWiFiProxies(c *echo.Context) error {
	proxies, err := h.proxies.load(c.Request().Context())
	if err != nil {
		return wifiProxyHTTPError(c, err)
	}
	summaries := make([]WiFiProxySummary, 0, len(proxies))
	for _, proxy := range proxies {
		summaries = append(summaries, proxy.summary())
	}
	return c.JSON(http.StatusOK, summaries)
}

// CreateWiFiProxy saves a new SOCKS5 proxy.
func (h *Handler) CreateWiFiProxy(c *echo.Context) error {
	return h.saveWiFiProxy(c, "", http.StatusCreated)
}

// UpdateWiFiProxy updates the configuration used by new Wi-Fi Calling connections.
func (h *Handler) UpdateWiFiProxy(c *echo.Context) error {
	return h.saveWiFiProxy(c, c.Param("proxyId"), http.StatusOK)
}

func (h *Handler) saveWiFiProxy(c *echo.Context, id string, status int) error {
	var request WiFiProxySettings
	if err := httpapi.BindJSON(c, &request, "invalid_request"); err != nil {
		return err
	}
	proxy, err := h.proxies.save(c.Request().Context(), id, request)
	if err != nil {
		return wifiProxyHTTPError(c, err)
	}
	return c.JSON(status, proxy)
}

// CheckWiFiProxy measures a saved proxy without modifying the active modem session.
func (h *Handler) CheckWiFiProxy(c *echo.Context) error {
	ctx := c.Request().Context()
	if _, err := h.registry.Find(ctx, c.Param("id")); err != nil {
		return httpapi.ModemLookupError(c, err, "internal_server_error")
	}
	var request struct {
		ProxyID string `json:"proxyId"`
	}
	if err := httpapi.BindJSON(c, &request, "invalid_request"); err != nil {
		return err
	}
	if strings.TrimSpace(request.ProxyID) == "" {
		return httpapi.UnprocessableEntity(c, "invalid_request", errInvalidWiFiProxy)
	}
	proxy, err := h.proxies.get(ctx, strings.TrimSpace(request.ProxyID))
	if err != nil {
		return wifiProxyHTTPError(c, err)
	}
	return c.JSON(http.StatusOK, checkWiFiProxy(ctx, socks5Underlay{proxy: proxy}))
}

func wifiProxyHTTPError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, errInvalidWiFiProxy):
		return httpapi.UnprocessableEntity(c, "invalid_request", err)
	case errors.Is(err, errWiFiProxyNotFound):
		return httpapi.NotFound(c, "resource_not_found", err)
	default:
		return httpapi.Internal(c, "internal_server_error", err)
	}
}
