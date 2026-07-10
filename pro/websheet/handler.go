//go:build esim_transfer || ims

package websheet

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
)

type Handler struct {
	broker *Broker
}

const (
	errorCodeWebsheetNotFound        = "websheet_not_found"
	errorCodeWebsheetExpired         = "websheet_expired"
	errorCodeWebsheetUnsafeURL       = "websheet_unsafe_url"
	errorCodeWebsheetProxyFailed     = "websheet_proxy_failed"
	errorCodeWebsheetCallbackInvalid = "websheet_callback_invalid"
)

func RegisterRoutes(group *echo.Group, broker *Broker) {
	h := &Handler{broker: broker}
	group.GET("/websheets/:id", h.Bootstrap)
	group.Match(proxyMethods, "/websheets/:id/proxy", h.Proxy)
	group.Match(proxyMethods, "/websheets/:id/proxy/*", h.Proxy)
	group.POST("/websheets/:id/callback", h.Callback)
	group.POST("/websheets/:id/done", h.Done)
}

var proxyMethods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
}

func (h *Handler) Bootstrap(c *echo.Context) error {
	session, err := h.session(c)
	if err != nil {
		return websheetError(c, err)
	}
	if err := session.ServeBootstrap(c.Response(), c.Request()); err != nil {
		return websheetError(c, err)
	}
	return nil
}

func (h *Handler) Proxy(c *echo.Context) error {
	session, err := h.session(c)
	if err != nil {
		return websheetError(c, err)
	}
	if err := session.Proxy(c.Response(), c.Request()); err != nil {
		return websheetError(c, err)
	}
	return nil
}

func (h *Handler) Callback(c *echo.Context) error {
	session, err := h.session(c)
	if err != nil {
		return websheetError(c, err)
	}
	var callback Callback
	if err := json.NewDecoder(c.Request().Body).Decode(&callback); err != nil {
		return httpapi.BadRequest(c, errorCodeWebsheetCallbackInvalid, err)
	}
	session.Callback(callback)
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Done(c *echo.Context) error {
	session, err := h.session(c)
	if err != nil {
		return websheetError(c, err)
	}
	session.Done()
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) session(c *echo.Context) (*Session, error) {
	if h == nil || h.broker == nil {
		return nil, ErrNotFound
	}
	return h.broker.Get(c.Param("id"))
}

func websheetError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return httpapi.NotFound(c, errorCodeWebsheetNotFound, err)
	case errors.Is(err, ErrExpired):
		return httpapi.Error(c, http.StatusGone, errorCodeWebsheetExpired, err.Error())
	case errors.Is(err, ErrUnsafeURL):
		return httpapi.BadRequest(c, errorCodeWebsheetUnsafeURL, err)
	default:
		return httpapi.Internal(c, errorCodeWebsheetProxyFailed, err)
	}
}
