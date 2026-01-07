package notify

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/config"
)

const defaultGotifyTitle = "Sigmo Notification"

type Gotify struct {
	client   *http.Client
	baseURL  url.URL
	tokens   []string
	title    string
	priority int
}

func NewGotify(cfg *config.Channel) (*Gotify, error) {
	parsed, err := parseEndpoint("gotify", cfg.Endpoint, "")
	if err != nil {
		return nil, err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if strings.Trim(parsed.Path, "/") == "" {
		parsed.Path = "/message"
	}
	query := parsed.Query()
	query.Del("token")
	parsed.RawQuery = query.Encode()
	tokens := cfg.Recipients.Strings()
	if len(tokens) == 0 {
		return nil, errors.New("gotify recipients are required")
	}
	return &Gotify{
		client:   &http.Client{Timeout: 10 * time.Second},
		baseURL:  *parsed,
		tokens:   tokens,
		title:    gotifyTitle(cfg.Subject),
		priority: cfg.Priority,
	}, nil
}

func gotifyTitle(raw string) string {
	title := strings.TrimSpace(raw)
	if title == "" {
		return defaultGotifyTitle
	}
	return title
}

func (g *Gotify) Send(message Message) error {
	if message == nil {
		return errors.New("gotify message is required")
	}
	body := strings.TrimSpace(message.String())
	if body == "" {
		return errors.New("gotify message is required")
	}
	var combined error
	for _, token := range g.tokens {
		if err := g.sendOne(token, body); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func (g *Gotify) sendOne(token string, body string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("gotify token is empty")
	}
	form := url.Values{}
	form.Set("message", body)
	if g.title != "" {
		form.Set("title", g.title)
	}
	if g.priority > 0 {
		form.Set("priority", strconv.Itoa(g.priority))
	}
	endpoint := g.baseURL
	query := endpoint.Query()
	query.Set("token", token)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequest(http.MethodPost, endpoint.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("building gotify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending gotify message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("gotify response status %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	return nil
}
