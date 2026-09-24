package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type Sender struct {
	client   *http.Client
	endpoint string
	headers  map[string]string
}

func New(channel *settings.Channel) (*Sender, error) {
	endpoint := strings.TrimSpace(channel.Endpoint)
	if endpoint == "" {
		return nil, errors.New("http endpoint is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing http endpoint: %w", err)
	}
	return &Sender{
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: parsed.String(),
		headers:  channel.Headers,
	}, nil
}

func (s *Sender) Send(ctx context.Context, msg notifycontent.Message) error {
	if msg.Event == nil {
		return errors.New("http event is required")
	}
	body, err := json.Marshal(struct {
		Kind    notifyevent.Kind  `json:"kind"`
		Payload notifyevent.Event `json:"payload"`
	}{
		Kind:    msg.Event.Kind(),
		Payload: msg.Event,
	})
	if err != nil {
		return fmt.Errorf("encoding http event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range s.headers {
		req.Header.Set(key, value)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending http message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("http response status %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}
	return nil
}
