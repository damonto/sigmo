package lark

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
}

type message struct {
	MsgType string  `json:"msg_type"`
	Content content `json:"content"`
}

type content struct {
	Text string `json:"text"`
}

func New(channel *settings.Channel) (*Sender, error) {
	endpoint := strings.TrimSpace(channel.Endpoint)
	if endpoint == "" {
		return nil, errors.New("lark endpoint is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing lark endpoint: %w", err)
	}
	return &Sender{
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: parsed.String(),
	}, nil
}

func (s *Sender) Send(ctx context.Context, ev notifyevent.Event) error {
	text, err := render(ev)
	if err != nil {
		return err
	}

	body, err := json.Marshal(message{MsgType: "text", Content: content{Text: text}})
	if err != nil {
		return fmt.Errorf("encoding lark message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building lark request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending lark message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("lark response status %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}
	return nil
}
