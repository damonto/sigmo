package internet

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	appconnectivity "github.com/damonto/sigmo/internal/app/connectivity"
	"github.com/damonto/sigmo/internal/app/httpapi"
	internetcore "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	registry    *mmodem.Registry
	connections appconnectivity.InternetConnections
}

func New(registry *mmodem.Registry, connections appconnectivity.InternetConnections) *Handler {
	return &Handler{
		registry:    registry,
		connections: connections,
	}
}

func (h *Handler) Current(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "current_internet_connection_failed")
	}
	response, err := h.connections.Current(ctx, modem)
	if err != nil {
		return internetError(c, err, "current_internet_connection_failed")
	}
	return c.JSON(http.StatusOK, ResponseFromConnection(response))
}

func (h *Handler) Public(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "internet_public_failed")
	}
	info, err := h.connections.Public(ctx, modem)
	if err != nil {
		return internetError(c, err, "internet_public_failed")
	}
	return c.JSON(http.StatusOK, ResponseFromPublic(info))
}

func (h *Handler) Connect(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "connect_internet_failed")
	}

	var req ConnectRequest
	if err := httpapi.BindAndValidate(c, &req, "connect_internet_invalid_request"); err != nil {
		return err
	}
	prefs := internetcore.Preferences{
		APN:          req.APN,
		IPType:       req.IPType,
		APNUsername:  req.APNUsername,
		APNPassword:  req.APNPassword,
		APNAuth:      req.APNAuth,
		DefaultRoute: req.DefaultRoute,
		ProxyEnabled: req.ProxyEnabled,
		AlwaysOn:     req.AlwaysOn,
	}
	if err := internetcore.ValidatePreferences(prefs); err != nil {
		return internetError(c, err, "connect_internet_failed")
	}
	response, err := h.connections.Connect(ctx, modem, prefs)
	if err != nil {
		return internetError(c, err, "connect_internet_failed")
	}
	return c.JSON(http.StatusCreated, ResponseFromConnection(response))
}

func (h *Handler) UpdatePreferences(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "internet_preferences_update_failed")
	}

	var req UpdatePreferencesRequest
	if err := httpapi.BindAndValidate(c, &req, "internet_preferences_invalid_request"); err != nil {
		return err
	}
	response, err := h.connections.UpdatePreferences(ctx, modem, internetcore.ConnectionPreferences{
		DefaultRoute: req.DefaultRoute,
		ProxyEnabled: req.ProxyEnabled,
		AlwaysOn:     req.AlwaysOn,
	})
	if err != nil {
		return internetError(c, err, "internet_preferences_update_failed")
	}
	return c.JSON(http.StatusOK, ResponseFromConnection(response))
}

func (h *Handler) Disconnect(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "disconnect_internet_failed")
	}
	if err := h.connections.Disconnect(ctx, modem); err != nil {
		return httpapi.Internal(c, "disconnect_internet_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func internetError(c *echo.Context, err error, internalErrorCode string) error {
	if errors.Is(err, internetcore.ErrOperationInProgress) {
		return httpapi.Error(c, http.StatusConflict, "internet_operation_in_progress", internetcore.ErrOperationInProgress.Error())
	}
	if errors.Is(err, internetcore.ErrAirplaneMode) {
		return httpapi.Error(c, http.StatusConflict, "internet_airplane_mode", internetcore.ErrAirplaneMode.Error())
	}
	if errors.Is(err, internetcore.ErrNotConnected) {
		return httpapi.Error(c, http.StatusConflict, "internet_connection_not_connected", internetcore.ErrNotConnected.Error())
	}
	if errors.Is(err, internetcore.ErrUnsupportedIPMethod) {
		return httpapi.UnprocessableEntity(c, "internet_connection_unsupported_ip_method", internetcore.ErrUnsupportedIPMethod)
	}
	if errors.Is(err, internetcore.ErrProxyPasswordRequired) {
		return httpapi.UnprocessableEntity(c, "internet_proxy_invalid_config", internetcore.ErrProxyPasswordRequired)
	}
	if errors.Is(err, internetcore.ErrProxyStart) {
		return httpapi.UnprocessableEntity(c, "internet_proxy_start_failed", err)
	}
	if errors.Is(err, internetcore.ErrProfileIDRequired) {
		return httpapi.UnprocessableEntity(c, "internet_profile_id_required", err)
	}
	if errors.Is(err, mmodem.ErrUnsupportedBearerAuth) {
		return httpapi.UnprocessableEntity(c, "internet_auth_invalid_config", err)
	}
	if errors.Is(err, mmodem.ErrUnsupportedBearerIPType) {
		return httpapi.UnprocessableEntity(c, "internet_ip_type_invalid_config", err)
	}
	return httpapi.Internal(c, internalErrorCode, err)
}

func ResponseFromConnection(connection *internetcore.Connection) ConnectionResponse {
	if connection == nil {
		return ConnectionResponse{
			IPv4Addresses: []string{},
			IPv6Addresses: []string{},
			DNS:           []string{},
		}
	}
	return ConnectionResponse{
		Status:          connection.Status,
		APN:             connection.APN,
		IPType:          connection.IPType,
		APNUsername:     connection.APNUsername,
		APNPassword:     connection.APNPassword,
		APNAuth:         connection.APNAuth,
		DefaultRoute:    connection.DefaultRoute,
		ProxyEnabled:    connection.ProxyEnabled,
		AlwaysOn:        connection.AlwaysOn,
		Proxy:           responseFromProxy(connection.Proxy),
		InterfaceName:   connection.InterfaceName,
		Bearer:          connection.Bearer,
		IPv4Addresses:   append([]string{}, connection.IPv4Addresses...),
		IPv6Addresses:   append([]string{}, connection.IPv6Addresses...),
		DNS:             append([]string{}, connection.DNS...),
		DurationSeconds: connection.DurationSeconds,
		TXBytes:         connection.TXBytes,
		RXBytes:         connection.RXBytes,
		RouteMetric:     connection.RouteMetric,
	}
}

func responseFromProxy(proxy internetcore.ProxyStatus) Proxy {
	return Proxy{
		Enabled:       proxy.Enabled,
		Username:      proxy.Username,
		Password:      proxy.Password,
		HTTPAddress:   proxy.HTTPAddress,
		SOCKS5Address: proxy.SOCKS5Address,
	}
}

func ResponseFromPublic(info internetcore.IPInfo) PublicResponse {
	return PublicResponse{
		IP:           info.IP,
		Country:      info.Country,
		Organization: info.Organization,
	}
}
