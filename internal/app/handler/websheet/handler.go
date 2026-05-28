package websheet

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	pwebsheet "github.com/damonto/sigmo/internal/pkg/websheet"
)

type Handler struct {
	broker *pwebsheet.Broker
}

const (
	errorCodeWebsheetNotFound        = "websheet_not_found"
	errorCodeWebsheetExpired         = "websheet_expired"
	errorCodeWebsheetUnsafeURL       = "websheet_unsafe_url"
	errorCodeWebsheetProxyFailed     = "websheet_proxy_failed"
	errorCodeWebsheetCallbackInvalid = "websheet_callback_invalid"
)

func New(broker *pwebsheet.Broker) *Handler {
	return &Handler{broker: broker}
}

func (h *Handler) Register(group *echo.Group) {
	group.GET("/websheets/:id", h.Bootstrap)
	group.GET("/websheets/:id/proxy", h.Proxy)
	group.POST("/websheets/:id/proxy", h.Proxy)
	group.PUT("/websheets/:id/proxy", h.Proxy)
	group.PATCH("/websheets/:id/proxy", h.Proxy)
	group.DELETE("/websheets/:id/proxy", h.Proxy)
	group.GET("/websheets/:id/proxy/*", h.Proxy)
	group.POST("/websheets/:id/proxy/*", h.Proxy)
	group.PUT("/websheets/:id/proxy/*", h.Proxy)
	group.PATCH("/websheets/:id/proxy/*", h.Proxy)
	group.DELETE("/websheets/:id/proxy/*", h.Proxy)
	group.POST("/websheets/:id/callback", h.Callback)
	group.POST("/websheets/:id/done", h.Done)
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
	var callback pwebsheet.Callback
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

func (h *Handler) session(c *echo.Context) (*pwebsheet.Session, error) {
	if h == nil || h.broker == nil {
		return nil, pwebsheet.ErrNotFound
	}
	return h.broker.Get(c.Param("id"))
}

func websheetError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, pwebsheet.ErrNotFound):
		return httpapi.NotFound(c, errorCodeWebsheetNotFound, err)
	case errors.Is(err, pwebsheet.ErrExpired):
		return httpapi.Error(c, http.StatusGone, errorCodeWebsheetExpired, err.Error())
	case errors.Is(err, pwebsheet.ErrUnsafeURL):
		return httpapi.BadRequest(c, errorCodeWebsheetUnsafeURL, err)
	default:
		return httpapi.Internal(c, errorCodeWebsheetProxyFailed, err)
	}
}
