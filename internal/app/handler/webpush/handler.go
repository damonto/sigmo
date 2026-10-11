package webpush

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/storage"
	push "github.com/damonto/sigmo/internal/pkg/webpush"
)

var errEnabledRequired = errors.New("enabled is required")

type Handler struct {
	client *push.Client
}

func New(client *push.Client) *Handler {
	return &Handler{client: client}
}

func (h *Handler) Get(c *echo.Context) error {
	overview, err := h.client.Overview(c.Request().Context())
	if err != nil {
		return httpapi.Internal(c, "read_web_push_failed", err)
	}
	return c.JSON(http.StatusOK, overviewFromService(overview))
}

func (h *Handler) Update(c *echo.Context) error {
	var req updateRequest
	if err := httpapi.BindJSON(c, &req, "update_web_push_failed"); err != nil {
		return err
	}
	if req.Enabled == nil {
		return httpapi.UnprocessableEntity(c, "update_web_push_failed", errEnabledRequired)
	}
	if err := h.client.SetEnabled(c.Request().Context(), *req.Enabled); err != nil {
		return httpapi.Internal(c, "update_web_push_failed", err)
	}
	return h.Get(c)
}

func (h *Handler) ListSubscriptions(c *echo.Context) error {
	overview, err := h.client.Overview(c.Request().Context())
	if err != nil {
		return httpapi.Internal(c, "read_web_push_failed", err)
	}
	return c.JSON(http.StatusOK, subscriptionsFromStorage(overview.Subscriptions))
}

func (h *Handler) Register(c *echo.Context) error {
	var req push.RegisterRequest
	if err := httpapi.BindJSON(c, &req, "register_web_push_subscription_invalid"); err != nil {
		return err
	}
	subscription, err := h.client.Register(c.Request().Context(), req, c.Request().UserAgent())
	if err != nil {
		if errors.Is(err, push.ErrInvalidSubscription) || errors.Is(err, push.ErrInvalidDeviceLabel) {
			return httpapi.UnprocessableEntity(c, "register_web_push_subscription_invalid", err)
		}
		return httpapi.Internal(c, "register_web_push_subscription_failed", err)
	}
	return c.JSON(http.StatusCreated, subscriptionFromStorage(subscription))
}

func (h *Handler) Rename(c *echo.Context) error {
	var req renameRequest
	if err := httpapi.BindJSON(c, &req, "rename_web_push_subscription_invalid"); err != nil {
		return err
	}
	subscription, err := h.client.Rename(c.Request().Context(), c.Param("subscriptionId"), req.Label)
	if err != nil {
		switch {
		case errors.Is(err, push.ErrInvalidDeviceLabel):
			return httpapi.UnprocessableEntity(c, "rename_web_push_subscription_invalid", err)
		case errors.Is(err, storage.ErrNotFound):
			return httpapi.NotFound(c, "web_push_subscription_not_found", err)
		default:
			return httpapi.Internal(c, "rename_web_push_subscription_failed", err)
		}
	}
	return c.JSON(http.StatusOK, subscriptionFromStorage(subscription))
}

func (h *Handler) Delete(c *echo.Context) error {
	id := strings.TrimSpace(c.Param("subscriptionId"))
	if err := h.client.Delete(c.Request().Context(), id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpapi.NotFound(c, "web_push_subscription_not_found", fmt.Errorf("push subscription %q: %w", id, err))
		}
		return httpapi.Internal(c, "delete_web_push_subscription_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func overviewFromService(overview push.Overview) overviewResponse {
	return overviewResponse{
		Enabled:       overview.Enabled,
		PublicKey:     overview.PublicKey,
		Subscriptions: subscriptionsFromStorage(overview.Subscriptions),
	}
}

func subscriptionsFromStorage(subscriptions []storage.PushSubscription) []subscriptionResponse {
	response := make([]subscriptionResponse, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		response = append(response, subscriptionFromStorage(subscription))
	}
	return response
}

func subscriptionFromStorage(subscription storage.PushSubscription) subscriptionResponse {
	return subscriptionResponse{
		ID:        subscription.ID,
		Endpoint:  subscription.Endpoint,
		Label:     subscription.Label,
		UserAgent: subscription.UserAgent,
		Platform:  subscription.Platform,
		CreatedAt: subscription.CreatedAt,
		UpdatedAt: subscription.UpdatedAt,
	}
}
