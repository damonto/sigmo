package internet

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	internetcore "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	manager   *mmodem.Manager
	connector *internetcore.Connector
}

const (
	errorCodeCurrentInternetConnectionFailed  = "current_internet_connection_failed"
	errorCodeInternetPublicFailed             = "internet_public_failed"
	errorCodeConnectInternetInvalidRequest    = "connect_internet_invalid_request"
	errorCodeConnectInternetFailed            = "connect_internet_failed"
	errorCodeDisconnectInternetFailed         = "disconnect_internet_failed"
	errorCodeUnsupportedInternetConfiguration = "internet_connection_unsupported_ip_method"
	errorCodeInternetProxyInvalidConfig       = "internet_proxy_invalid_config"
	errorCodeInternetProxyStartFailed         = "internet_proxy_start_failed"
	errorCodeInternetAuthInvalidConfig        = "internet_auth_invalid_config"
	errorCodeInternetIPTypeInvalidConfig      = "internet_ip_type_invalid_config"
)

func New(manager *mmodem.Manager) *Handler {
	return NewWithConnector(manager, internetcore.NewConnector())
}

func NewWithConnector(manager *mmodem.Manager, connector *internetcore.Connector) *Handler {
	return &Handler{
		manager:   manager,
		connector: connector,
	}
}

func (h *Handler) Current(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeCurrentInternetConnectionFailed)
	}
	response, err := h.connector.Current(modem)
	if err != nil {
		return internetError(c, err, errorCodeCurrentInternetConnectionFailed)
	}
	return c.JSON(http.StatusOK, responseFromConnection(response))
}

func (h *Handler) Public(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeInternetPublicFailed)
	}
	info, err := h.connector.Public(c.Request().Context(), modem)
	if err != nil {
		return internetError(c, err, errorCodeInternetPublicFailed)
	}
	return c.JSON(http.StatusOK, responseFromPublic(info))
}

func (h *Handler) Connect(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeConnectInternetFailed)
	}

	var req ConnectRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeConnectInternetInvalidRequest); err != nil {
		return err
	}
	if _, err := mmodem.BearerAllowedAuth(req.APNAuth); err != nil {
		return httpapi.UnprocessableEntity(c, errorCodeInternetAuthInvalidConfig, err)
	}
	if _, err := mmodem.BearerIPFamily(req.IPType); err != nil {
		return httpapi.UnprocessableEntity(c, errorCodeInternetIPTypeInvalidConfig, err)
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
	response, err := h.connector.Connect(modem, prefs)
	if err != nil {
		return internetError(c, err, errorCodeConnectInternetFailed)
	}
	return c.JSON(http.StatusCreated, responseFromConnection(response))
}

func (h *Handler) Disconnect(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeDisconnectInternetFailed)
	}
	if err := h.connector.Disconnect(modem); err != nil {
		return httpapi.Internal(c, errorCodeDisconnectInternetFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func internetError(c *echo.Context, err error, internalErrorCode string) error {
	if errors.Is(err, internetcore.ErrUnsupportedIPMethod) {
		return httpapi.UnprocessableEntity(c, errorCodeUnsupportedInternetConfiguration, internetcore.ErrUnsupportedIPMethod)
	}
	if errors.Is(err, internetcore.ErrProxyPasswordRequired) {
		return httpapi.UnprocessableEntity(c, errorCodeInternetProxyInvalidConfig, internetcore.ErrProxyPasswordRequired)
	}
	if errors.Is(err, internetcore.ErrProxyStart) {
		return httpapi.UnprocessableEntity(c, errorCodeInternetProxyStartFailed, err)
	}
	if errors.Is(err, mmodem.ErrUnsupportedBearerAuth) {
		return httpapi.UnprocessableEntity(c, errorCodeInternetAuthInvalidConfig, err)
	}
	if errors.Is(err, mmodem.ErrUnsupportedBearerIPType) {
		return httpapi.UnprocessableEntity(c, errorCodeInternetIPTypeInvalidConfig, err)
	}
	return httpapi.Internal(c, internalErrorCode, err)
}

func responseFromConnection(connection *internetcore.Connection) ConnectionResponse {
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
		IPv4Addresses:   connection.IPv4Addresses,
		IPv6Addresses:   connection.IPv6Addresses,
		DNS:             connection.DNS,
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

func responseFromPublic(info internetcore.IPInfo) PublicResponse {
	return PublicResponse{
		IP:           info.IP,
		Country:      info.Country,
		Organization: info.Organization,
	}
}
