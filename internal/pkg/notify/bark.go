package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/config"
)

const defaultBarkEndpoint = "https://api.day.app/push"
const defaultBarkTitle = "Sigmo Notification"

type Bark struct {
	client     *http.Client
	endpoint   string
	deviceKeys []string
	title      string
}

type barkPayload struct {
	Title     string `json:"title,omitempty"`
	Body      string `json:"body"`
	DeviceKey string `json:"device_key"`
}

func NewBark(cfg *config.Channel) (*Bark, error) {
	parsed, err := parseEndpoint("bark", cfg.Endpoint, defaultBarkEndpoint)
	if err != nil {
		return nil, err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if strings.Trim(parsed.Path, "/") == "" {
		parsed.Path = "/push"
	}
	deviceKeys := cfg.Recipients.Strings()
	if len(deviceKeys) == 0 {
		return nil, errors.New("bark recipients are required")
	}
	return &Bark{
		client:     &http.Client{Timeout: 10 * time.Second},
		endpoint:   parsed.String(),
		deviceKeys: deviceKeys,
		title:      barkTitle(cfg.Subject),
	}, nil
}

func barkTitle(raw string) string {
	title := strings.TrimSpace(raw)
	if title == "" {
		return defaultBarkTitle
	}
	return title
}

func (b *Bark) Send(message Message) error {
	if message == nil {
		return errors.New("bark message is required")
	}
	body := strings.TrimSpace(message.String())
	if body == "" {
		return errors.New("bark body is required")
	}
	var combined error
	for _, deviceKey := range b.deviceKeys {
		if err := b.sendOne(deviceKey, body); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func (b *Bark) sendOne(deviceKey string, body string) error {
	deviceKey = strings.TrimSpace(deviceKey)
	if deviceKey == "" {
		return errors.New("bark device key is empty")
	}
	payload := barkPayload{
		Title:     b.title,
		Body:      body,
		DeviceKey: deviceKey,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding bark message: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, b.endpoint, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("building bark request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending bark message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("bark response status %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	return nil
}
