package nativepush

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/damonto/sigmo/internal/pkg/apierror"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// GatewayURL may be set by official release builds; subscriptions cannot override it.
var GatewayURL = "https://sigmo-pro-api.id.workers.dev"

// Config supplies backend-owned state and a transport replaceable in integration tests.
type Config struct {
	Storage    *storage.Store
	Settings   *settings.Store
	URL        string
	Client     *http.Client
	Authorized func() bool
}

// Dispatcher owns the lifecycle of durable notification retries.
type Dispatcher struct {
	store      *storage.Store
	settings   *settings.Store
	endpoint   string
	client     *http.Client
	authorized func() bool
}

// New configures a bounded HTTPS-only gateway transport.
func New(cfg Config) (*Dispatcher, error) {
	if cfg.Storage == nil {
		return nil, errors.New("native push storage is required")
	}
	if cfg.URL == "" {
		cfg.URL = GatewayURL
	}
	endpoint, err := url.Parse(cfg.URL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("invalid native push gateway URL")
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &Dispatcher{
		store:      cfg.Storage,
		settings:   cfg.Settings,
		endpoint:   strings.TrimRight(cfg.URL, "/") + "/v1/push-deliveries",
		client:     cfg.Client,
		authorized: cfg.Authorized,
	}, nil
}

// Run stops all requests and workers when the server context is canceled.
func (d *Dispatcher) Run(ctx context.Context) error {
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return d.runNotifications(ctx) })
	group.Go(func() error { return d.runCalls(ctx) })
	return group.Wait()
}

func (d *Dispatcher) runNotifications(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := d.drain(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("dispatch native notifications", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) drain(ctx context.Context) error {
	items, err := d.store.NativeDeliveries(ctx, time.Now())
	if err != nil {
		return err
	}
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for _, item := range items {
		group.Go(func() error {
			if err := d.deliver(ctx, item); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				retry, retryAt := deliveryRetry(err)
				slog.Warn("deliver native notification", "kind", item.Content.Kind, "error", err, "retry", retry)
				if retry {
					return d.store.RetryNativeDelivery(ctx, item, retryAt)
				}
			}
			return d.store.FinishNativeDelivery(ctx, item)
		})
	}
	return group.Wait()
}

type deliveryRequest struct {
	CallUUID       string    `json:"callUUID,omitempty"`
	SubscriptionID string    `json:"subscriptionId"`
	EventID        string    `json:"eventId"`
	Kind           string    `json:"kind"`
	Revision       int64     `json:"revision"`
	UnreadCount    int       `json:"unreadCount"`
	ExpiresAt      int64     `json:"expiresAt"`
	ThreadID       string    `json:"threadId,omitempty"`
	Envelope       *Envelope `json:"envelope,omitempty"`
}

func (d *Dispatcher) deliver(ctx context.Context, item storage.NativeDelivery) error {
	// Preferences or credentials can change while an item waits for a worker.
	sub, err := d.store.NativeSubscription(ctx, item.Subscription.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	valid := d.authorized == nil || d.authorized()
	if valid && d.settings != nil && d.settings.OTPRequired() {
		var err error
		valid, err = d.store.AuthTokenValid(ctx, sub.AuthTokenHash, time.Now())
		if err != nil {
			return err
		}
	}
	if item.Content.Kind != "badge" {
		if !valid || !sub.Enabled {
			return nil
		}
		switch item.Content.Kind {
		case "sms":
			if !sub.SMS {
				return nil
			}
			conversation, err := d.store.Conversation(ctx, item.Content.ConversationID)
			if errors.Is(err, storage.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			if conversation.ReadThroughMessageID >= item.Content.MessageID {
				return nil
			}
		case "reminder":
			if !sub.Reminders {
				return nil
			}
		case "call":
			if !sub.Calls {
				return nil
			}
			call, err := d.store.Call(ctx, item.Content.CallID)
			if errors.Is(err, storage.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			if call.State != "ringing" || call.Direction != "incoming" ||
				call.OwnerClientID != "" || !time.Now().Before(item.ExpiresAt) {
				return nil
			}
		}
	}
	state, err := d.store.MessagesState(ctx)
	if err != nil {
		return err
	}
	if !valid || !sub.Enabled || !sub.SMS {
		state.UnreadCount = 0
	}
	body := deliveryRequest{
		SubscriptionID: sub.ID,
		EventID:        item.EventID,
		Kind:           item.Content.Kind,
		Revision:       state.Revision,
		UnreadCount:    state.UnreadCount,
		ExpiresAt:      item.ExpiresAt.Unix(),
		CallUUID:       item.Content.CallUUID,
	}
	if item.Content.Kind == "badge" {
		body.EventID += ":" + strconv.FormatInt(item.Revision, 10)
	}
	if item.Content.Kind != "badge" {
		item.Content.BackendID, item.Content.BackendName = state.BackendID, sub.BackendName
		if d.settings != nil {
			item.Content.ModemName = d.settings.FindModem(item.Content.ModemID).Alias
		}
		envelope, err := Encrypt(Encryption{
			Key:            sub.EncryptionKey,
			SubscriptionID: sub.ID,
			EventID:        body.EventID,
			Content:        item.Content,
		})
		if err != nil {
			return err
		}
		body.Envelope = &envelope
		sum := sha256.Sum256([]byte(state.BackendID + ":" + item.Content.ProfileID + ":" + item.Content.ConversationID))
		body.ThreadID = hex.EncodeToString(sum[:16])
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode gateway delivery: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create gateway request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, "+apierror.MediaType)
	req.Header.Set("Authorization", "Bearer "+sub.SendToken)
	response, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("send gateway request: %w", err)
	}
	defer func() { _ = response.Body.Close() }() // The status and body read determine delivery success.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		remote := apierror.Read(response)
		action := gatewayAction(remote)
		if action == deliveryRevoke {
			removed, err := d.store.DeleteNativeSubscriptionIfCurrent(ctx, sub)
			if err != nil {
				return err
			}
			if removed {
				slog.Warn("revoke native push subscription", "status", remote.StatusCode,
					"error_code", remote.ErrorCode, "request_id", remote.RequestID)
				return nil
			}
			// A credential rotated while this attempt was in flight. Try the
			// current grant without treating an old response as its revocation.
			action = deliveryRetryLater
		}
		return &deliveryFailure{remote: remote, action: action}
	}
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 8192)); err != nil {
		return fmt.Errorf("read gateway response: %w", err)
	}
	if !valid {
		_, err := d.store.DeleteNativeSubscriptionIfCurrent(ctx, sub)
		return err
	}
	return nil
}

type deliveryAction uint8

const (
	deliveryDiscard deliveryAction = iota
	deliveryRevoke
	deliveryRetryLater
)

type deliveryFailure struct {
	remote *apierror.Error
	action deliveryAction
}

// Error includes gateway metadata without exposing response details or credentials.
func (e *deliveryFailure) Error() string {
	return fmt.Sprintf("gateway HTTP %d (%s), request %s", e.remote.StatusCode, e.remote.ErrorCode, e.remote.RequestID)
}

// Unwrap preserves the HTTP failure for retry decisions and diagnostics.
func (e *deliveryFailure) Unwrap() error { return e.remote }

func deliveryRetry(err error) (bool, time.Time) {
	var failure *deliveryFailure
	if errors.As(err, &failure) {
		return failure.action == deliveryRetryLater, failure.remote.RetryAt
	}
	return true, time.Time{}
}

func gatewayAction(remote *apierror.Error) deliveryAction {
	if remote.ParseErr == nil &&
		((remote.StatusCode == http.StatusUnauthorized && remote.ErrorCode == "invalid_push_credentials") ||
			(remote.StatusCode == http.StatusGone && remote.ErrorCode == "push_token_unregistered")) {
		return deliveryRevoke
	}
	if remote.StatusCode == http.StatusConflict && remote.ErrorCode == "push_delivery_in_progress" {
		return deliveryRetryLater
	}
	if remote.StatusCode == http.StatusBadGateway && remote.ErrorCode == "push_provider_rejected" {
		return deliveryDiscard
	}
	if remote.StatusCode >= 500 && remote.StatusCode != http.StatusNotImplemented && remote.StatusCode != http.StatusHTTPVersionNotSupported {
		return deliveryRetryLater
	}
	switch remote.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusRequestTimeout:
		return deliveryRetryLater
	default:
		return deliveryDiscard
	}
}
