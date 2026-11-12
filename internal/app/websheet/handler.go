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

// NewHandler creates HTTP handlers for capability-scoped carrier pages.
func NewHandler(broker *Broker) *Handler {
	return &Handler{broker: broker}
}

// Capability confines an untrusted carrier document to one expiring random ID.
// The outer router still verifies Access assertions when Access owns the boundary.
func (h *Handler) Capability(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if _, err := h.session(c); err != nil {
			return websheetError(c, err)
		}
		headers := c.Response().Header()
		headers.Set("Cache-Control", "no-store")
		headers.Set("Referrer-Policy", "no-referrer")
		// Never grant the carrier document Sigmo's browser origin, even if opened directly.
		headers.Set("Content-Security-Policy", "sandbox allow-scripts allow-forms; frame-ancestors 'self'; object-src 'none'")
		if c.Request().Header.Get("Origin") == "null" {
			headers.Set("Access-Control-Allow-Origin", "null")
			headers.Set("Access-Control-Allow-Credentials", "true")
			headers.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			headers.Set("Access-Control-Allow-Headers", "Content-Type, Accept")
			headers.Add("Vary", "Origin")
		}
		return next(c)
	}
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
		return httpapi.BadRequest(c, "websheet_callback_invalid", err)
	}
	if err := session.Callback(callback); err != nil {
		return websheetError(c, err)
	}
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
		return httpapi.NotFound(c, "websheet_not_found", err)
	case errors.Is(err, ErrExpired):
		return httpapi.Error(c, http.StatusGone, "websheet_expired", err.Error())
	case errors.Is(err, ErrUnsafeURL):
		return httpapi.BadRequest(c, "websheet_unsafe_url", err)
	case errors.Is(err, ErrCallbackQueueFull):
		return httpapi.TooManyRequests(c, "too_many_requests", err)
	default:
		return httpapi.Internal(c, "websheet_proxy_failed", err)
	}
}
