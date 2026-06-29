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

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type Sender struct {
	client   *http.Client
	endpoint string
	headers  map[string]string
	format   string
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
		format:   strings.ToLower(strings.TrimSpace(channel.Format)),
	}, nil
}

func (s *Sender) Send(ctx context.Context, ev notifyevent.Event) error {
	body, err := s.body(ev)
	if err != nil {
		return err
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

// body builds the request payload. Feishu and WeCom share a simple text message
// shape (only the field names differ); any other format sends the default
// {kind, payload} document.
func (s *Sender) body(ev notifyevent.Event) ([]byte, error) {
	switch s.format {
	case "feishu":
		return json.Marshal(map[string]any{
			"msg_type": "text",
			"content":  map[string]string{"text": renderText(ev)},
		})
	case "wecom":
		return json.Marshal(map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": renderText(ev)},
		})
	default:
		return json.Marshal(struct {
			Kind    notifyevent.Kind  `json:"kind"`
			Payload notifyevent.Event `json:"payload"`
		}{
			Kind:    ev.Kind(),
			Payload: ev,
		})
	}
}

func renderText(ev notifyevent.Event) string {
	switch ev := ev.(type) {
	case notifyevent.OTPEvent:
		return fmt.Sprintf("Sigmo Login\nYour verification code is %s", strings.TrimSpace(ev.Code))
	case notifyevent.SMSEvent:
		return fmt.Sprintf("%s\n%s", ev.DisplayCounterparty(), ev.DisplayText())
	case notifyevent.CallEvent:
		number := ev.DisplayCounterparty()
		title := ev.DirectionLabel()
		if number != "" {
			title = fmt.Sprintf("%s from %s", title, number)
		}
		return fmt.Sprintf("%s\nModem: %s\nTime: %s", title, strings.TrimSpace(ev.Modem), ev.DisplayTimestamp())
	default:
		return ""
	}
}
