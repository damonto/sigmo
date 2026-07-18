package webpush

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	stateScope       = "web_push"
	stateKey         = "config"
	defaultSubject   = "https://github.com/damonto/sigmo"
	maxDeviceLabel   = 64
	maxPlatformLabel = 64
	maxSMSPreview    = 240
	maxReminderBody  = 240
)

var (
	ErrInvalidSubscription = errors.New("invalid push subscription")
	ErrInvalidDeviceLabel  = errors.New("device label must be between 1 and 64 characters")
)

type Client struct {
	store      *storage.Store
	httpClient httpClient

	mu         sync.RWMutex
	enabled    bool
	privateKey *ecdsa.PrivateKey
}

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

type config struct {
	Enabled    *bool           `json:"enabled,omitempty"`
	PrivateKey vapidPrivateKey `json:"privateKey"`
}

type Overview struct {
	Enabled       bool                       `json:"enabled"`
	PublicKey     string                     `json:"publicKey"`
	Subscriptions []storage.PushSubscription `json:"subscriptions"`
}

type RegisterRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Label    string `json:"label"`
	Platform string `json:"platform"`
}

func New(ctx context.Context, store *storage.Store) (*Client, error) {
	if store == nil {
		return nil, errors.New("storage is required")
	}

	var current config
	err := store.Get(ctx, stateScope, stateKey, &current)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("load web push config: %w", err)
	}
	privateKey := current.PrivateKey.PrivateKey
	if privateKey == nil {
		privateKey, err = generateVAPIDKey()
		if err != nil {
			return nil, fmt.Errorf("generate VAPID key: %w", err)
		}
	}
	enabled := true
	if current.Enabled != nil {
		enabled = *current.Enabled
	}
	client := &Client{
		store:      store,
		httpClient: newPushHTTPClient(),
		enabled:    enabled,
		privateKey: privateKey,
	}
	if current.PrivateKey.PrivateKey == nil || current.Enabled == nil {
		if err := client.saveConfig(ctx); err != nil {
			return nil, err
		}
	}
	return client, nil
}

func (c *Client) setHTTPClient(client httpClient) {
	if client == nil {
		return
	}
	c.mu.Lock()
	c.httpClient = client
	c.mu.Unlock()
}

func (c *Client) Overview(ctx context.Context) (Overview, error) {
	c.mu.RLock()
	enabled := c.enabled
	privateKey := c.privateKey
	c.mu.RUnlock()
	publicKey, err := vapidPublicKey(privateKey)
	if err != nil {
		return Overview{}, fmt.Errorf("encode VAPID public key: %w", err)
	}
	subscriptions, err := c.store.ListPushSubscriptions(ctx)
	if err != nil {
		return Overview{}, err
	}
	return Overview{Enabled: enabled, PublicKey: publicKey, Subscriptions: subscriptions}, nil
}

func (c *Client) SetEnabled(ctx context.Context, enabled bool) error {
	c.mu.Lock()
	previous := c.enabled
	c.enabled = enabled
	err := c.saveConfigLocked(ctx)
	if err != nil {
		c.enabled = previous
	}
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("save web push state: %w", err)
	}
	return nil
}

func (c *Client) Register(ctx context.Context, req RegisterRequest, userAgent string) (storage.PushSubscription, error) {
	endpoint, p256dh, auth, label, platform, err := validateRegistration(req)
	if err != nil {
		return storage.PushSubscription{}, err
	}
	id, err := randomID()
	if err != nil {
		return storage.PushSubscription{}, fmt.Errorf("generate push subscription id: %w", err)
	}
	return c.store.UpsertPushSubscription(ctx, storage.PushSubscription{
		ID:        id,
		Endpoint:  endpoint,
		P256DH:    p256dh,
		Auth:      auth,
		Label:     label,
		Platform:  platform,
		UserAgent: truncate(userAgent, maxPlatformLabel),
	})
}

func (c *Client) Rename(ctx context.Context, id, label string) (storage.PushSubscription, error) {
	label, err := normalizeLabel(label)
	if err != nil {
		return storage.PushSubscription{}, err
	}
	return c.store.RenamePushSubscription(ctx, id, label)
}

func (c *Client) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return storage.ErrNotFound
	}
	return c.store.DeletePushSubscription(ctx, id)
}

func (c *Client) Send(ctx context.Context, event notifyevent.Event) error {
	payload, ttl, priority, ok := payloadForEvent(event)
	if !ok {
		return nil
	}
	c.mu.RLock()
	if !c.enabled {
		c.mu.RUnlock()
		return nil
	}
	privateKey := c.privateKey
	httpClient := c.httpClient
	c.mu.RUnlock()
	if privateKey == nil || httpClient == nil {
		return errors.New("web push client is not ready")
	}
	subscriptions, err := c.store.ListPushSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("list push subscriptions for send: %w", err)
	}
	if len(subscriptions) == 0 {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode web push payload: %w", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var combined error
	for _, item := range subscriptions {
		item := item
		wg.Go(func() {
			if err := c.sendOne(ctx, httpClient, privateKey, item, body, ttl, priority); err != nil {
				mu.Lock()
				combined = errors.Join(combined, fmt.Errorf("send web push to %s: %w", item.ID, err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return combined
}

func (c *Client) sendOne(
	ctx context.Context,
	client httpClient,
	privateKey *ecdsa.PrivateKey,
	item storage.PushSubscription,
	payload []byte,
	ttl int,
	priority urgency,
) error {
	keys, err := decodeSubscriptionKeys(item.Auth, item.P256DH)
	if err != nil {
		return fmt.Errorf("decode subscription keys: %w", err)
	}
	req, err := buildPushRequest(ctx, item.Endpoint, payload, keys, privateKey, ttl, priority, time.Now())
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send push request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		if err := c.store.DeletePushSubscription(ctx, item.ID); err != nil && !errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("remove expired subscription after HTTP %d: %w", resp.StatusCode, err)
		}
		return nil
	}
	message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("push service returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
}

type notificationPayload struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	ModemID     string `json:"modemId"`
	Modem       string `json:"modem"`
	From        string `json:"from,omitempty"`
	Text        string `json:"text,omitempty"`
	ProfileType string `json:"profileType,omitempty"`
	ProfileID   string `json:"profileId,omitempty"`
	ProfileName string `json:"profileName,omitempty"`
	URL         string `json:"url"`
	Tag         string `json:"tag"`
}

func payloadForEvent(event notifyevent.Event) (notificationPayload, int, urgency, bool) {
	switch event := event.(type) {
	case notifyevent.SMSEvent:
		if !event.Incoming || strings.TrimSpace(event.ModemID) == "" || strings.TrimSpace(event.ID) == "" {
			return notificationPayload{}, 0, "", false
		}
		from := strings.TrimSpace(event.From)
		return notificationPayload{
			Type:    string(notifyevent.KindSMS),
			ID:      event.ID,
			ModemID: event.ModemID,
			Modem:   strings.TrimSpace(event.Modem),
			From:    from,
			Text:    truncate(strings.TrimSpace(event.Text), maxSMSPreview),
			URL:     "/modems/" + url.PathEscape(event.ModemID) + "/messages/" + url.PathEscape(from),
			Tag:     "sms:" + event.ID,
		}, 3600, urgencyNormal, true
	case notifyevent.CallEvent:
		if !event.Incoming || event.State != "ringing" || strings.TrimSpace(event.ModemID) == "" || strings.TrimSpace(event.ID) == "" {
			return notificationPayload{}, 0, "", false
		}
		return notificationPayload{
			Type:    string(notifyevent.KindCall),
			ID:      event.ID,
			ModemID: event.ModemID,
			Modem:   strings.TrimSpace(event.Modem),
			From:    strings.TrimSpace(event.From),
			URL:     "/modems/" + url.PathEscape(event.ModemID) + "/phone",
			Tag:     "call:" + event.ID,
		}, 60, urgencyHigh, true
	case notifyevent.ReminderEvent:
		if strings.TrimSpace(event.ModemID) == "" || strings.TrimSpace(event.ProfileID) == "" {
			return notificationPayload{}, 0, "", false
		}
		id := event.ProfileType + ":" + event.ProfileID + ":" + event.ScheduledAt.UTC().Format(time.RFC3339Nano)
		return notificationPayload{
			Type:        string(notifyevent.KindReminder),
			ID:          id,
			ModemID:     event.ModemID,
			Modem:       strings.TrimSpace(event.Modem),
			Text:        truncate(strings.TrimSpace(event.Content), maxReminderBody),
			ProfileType: strings.TrimSpace(event.ProfileType),
			ProfileID:   strings.TrimSpace(event.ProfileID),
			ProfileName: strings.TrimSpace(event.ProfileName),
			URL:         "/modems/" + url.PathEscape(event.ModemID),
			Tag:         "reminder:" + event.ProfileType + ":" + event.ProfileID,
		}, 86400, urgencyNormal, true
	default:
		return notificationPayload{}, 0, "", false
	}
}

func validateRegistration(req RegisterRequest) (string, string, string, string, string, error) {
	endpoint := strings.TrimSpace(req.Endpoint)
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", "", "", "", "", fmt.Errorf("%w: endpoint must be an HTTPS URL", ErrInvalidSubscription)
	}
	if addr, err := netip.ParseAddr(parsed.Hostname()); err == nil && !isPublicPushAddress(addr) {
		return "", "", "", "", "", fmt.Errorf("%w: endpoint must use a public address", ErrInvalidSubscription)
	}
	p256dh := strings.TrimSpace(req.Keys.P256DH)
	auth := strings.TrimSpace(req.Keys.Auth)
	if _, err := decodeSubscriptionKeys(auth, p256dh); err != nil {
		return "", "", "", "", "", fmt.Errorf("%w: invalid subscription keys: %v", ErrInvalidSubscription, err)
	}
	label, err := normalizeLabel(req.Label)
	if err != nil {
		return "", "", "", "", "", err
	}
	platform := truncate(strings.TrimSpace(req.Platform), maxPlatformLabel)
	return endpoint, p256dh, auth, label, platform, nil
}

func normalizeLabel(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > maxDeviceLabel {
		return "", ErrInvalidDeviceLabel
	}
	return value, nil
}

func truncate(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (c *Client) saveConfig(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.saveConfigLocked(ctx)
}

func (c *Client) saveConfigLocked(ctx context.Context) error {
	enabled := c.enabled
	return c.store.Put(ctx, stateScope, stateKey, config{
		Enabled:    &enabled,
		PrivateKey: vapidPrivateKey{PrivateKey: c.privateKey},
	})
}
