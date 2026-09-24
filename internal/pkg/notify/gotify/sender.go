package gotify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type Sender struct {
	client   *http.Client
	baseURL  url.URL
	tokens   []string
	priority int
}

func New(channel *settings.Channel) (*Sender, error) {
	endpoint := strings.TrimSpace(channel.Endpoint)
	if endpoint == "" {
		return nil, errors.New("gotify endpoint is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing gotify endpoint: %w", err)
	}
	appendPathSegment(parsed, "message")
	query := parsed.Query()
	query.Del("token")
	parsed.RawQuery = query.Encode()
	tokens := channel.Recipients.Strings()
	if len(tokens) == 0 {
		return nil, errors.New("gotify recipients are required")
	}
	return &Sender{
		client:   &http.Client{Timeout: 10 * time.Second},
		baseURL:  *parsed,
		tokens:   tokens,
		priority: channel.Priority,
	}, nil
}

func (s *Sender) Send(ctx context.Context, msg notifycontent.Message) error {
	content := render(msg)
	if strings.TrimSpace(content.Body) == "" {
		return errors.New("gotify message is required")
	}

	var combined error
	for _, token := range s.tokens {
		if err := s.sendOne(ctx, token, content); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func (s *Sender) sendOne(ctx context.Context, token string, content content) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("gotify token is empty")
	}

	form := url.Values{}
	form.Set("message", content.Body)
	if content.Title != "" {
		form.Set("title", content.Title)
	}
	if s.priority > 0 {
		form.Set("priority", strconv.Itoa(s.priority))
	}

	endpoint := s.baseURL
	query := endpoint.Query()
	query.Set("token", token)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("building gotify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending gotify message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("gotify response status %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
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
