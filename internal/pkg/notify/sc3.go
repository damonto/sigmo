package notify

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/config"
)

const defaultSC3Title = "Sigmo Notification"

type SC3 struct {
	client   *http.Client
	endpoint string
	title    string
}

func NewSC3(cfg *config.Channel) (*SC3, error) {
	parsed, err := parseEndpoint("sc3", cfg.Endpoint, "")
	if err != nil {
		return nil, err
	}
	if strings.Trim(parsed.Path, "/") == "" {
		return nil, errors.New("sc3 endpoint must include sendkey path")
	}
	return &SC3{
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: parsed.String(),
		title:    sc3Title(cfg.Subject),
	}, nil
}

func sc3Title(raw string) string {
	title := strings.TrimSpace(raw)
	if title == "" {
		return defaultSC3Title
	}
	return title
}

func (s *SC3) Send(message Message) error {
	if message == nil {
		return errors.New("sc3 message is required")
	}
	body := strings.TrimSpace(message.String())
	if body == "" {
		return errors.New("sc3 message is required")
	}
	form := url.Values{}
	form.Set("title", s.title)
	form.Set("desp", body)
	req, err := http.NewRequest(http.MethodPost, s.endpoint, strings.NewReader(form.Encode()))
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
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("sc3 response status %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	return nil
}
