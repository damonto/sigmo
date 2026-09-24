package bark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

const defaultEndpoint = "https://api.day.app"

type Sender struct {
	client     *http.Client
	endpoint   string
	deviceKeys []string
}

type payload struct {
	Title     string `json:"title,omitempty"`
	Body      string `json:"body"`
	Icon      string `json:"icon,omitempty"`
	DeviceKey string `json:"device_key"`
}

func New(channel *settings.Channel) (*Sender, error) {
	endpoint := strings.TrimSpace(channel.Endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing bark endpoint: %w", err)
	}
	appendPathSegment(parsed, "push")
	deviceKeys := channel.Recipients.Strings()
	if len(deviceKeys) == 0 {
		return nil, errors.New("bark recipients are required")
	}
	return &Sender{
		client:     &http.Client{Timeout: 10 * time.Second},
		endpoint:   parsed.String(),
		deviceKeys: deviceKeys,
	}, nil
}

func (s *Sender) Send(ctx context.Context, msg notifycontent.Message) error {
	content := render(msg)
	if strings.TrimSpace(content.Body) == "" {
		return errors.New("bark body is required")
	}

	var combined error
	for _, deviceKey := range s.deviceKeys {
		if err := s.sendOne(ctx, deviceKey, content); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func (s *Sender) sendOne(ctx context.Context, deviceKey string, content content) error {
	deviceKey = strings.TrimSpace(deviceKey)
	if deviceKey == "" {
		return errors.New("bark device key is empty")
	}

	body, err := json.Marshal(payload{
		Title:     content.Title,
		Body:      content.Body,
		Icon:      content.Icon,
		DeviceKey: deviceKey,
	})
	if err != nil {
		return fmt.Errorf("encoding bark message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building bark request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending bark message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("bark response status %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func appendPathSegment(parsed *url.URL, segment string) {
	trimmed := strings.TrimRight(parsed.Path, "/")
	if trimmed == "" {
		parsed.Path = "/" + segment
		return
	}
	if strings.HasSuffix(trimmed, "/"+segment) {
		parsed.Path = trimmed
		return
	}
	parsed.Path = path.Join(trimmed, segment)
}
