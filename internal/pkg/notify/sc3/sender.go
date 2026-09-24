package sc3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type Sender struct {
	client   *http.Client
	endpoint string
}

func New(channel *settings.Channel) (*Sender, error) {
	endpoint := strings.TrimSpace(channel.Endpoint)
	if endpoint == "" {
		return nil, errors.New("sc3 endpoint is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing sc3 endpoint: %w", err)
	}
	if strings.Trim(parsed.Path, "/") == "" {
		return nil, errors.New("sc3 endpoint must include sendkey path")
	}
	return &Sender{
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: parsed.String(),
	}, nil
}

func (s *Sender) Send(ctx context.Context, msg notifycontent.Message) error {
	content := render(msg)
	if strings.TrimSpace(content.Body) == "" {
		return errors.New("sc3 message is required")
	}

	form := url.Values{}
	form.Set("title", content.Title)
	form.Set("desp", content.Body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("building sc3 request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending sc3 message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("sc3 response status %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}
	return nil
}
