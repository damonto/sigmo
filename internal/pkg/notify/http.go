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

type HTTP struct {
	client   *http.Client
	endpoint string
	headers  map[string]string
}

func NewHTTP(cfg *config.Channel) (*HTTP, error) {
	parsed, err := parseEndpoint("http", cfg.Endpoint, "")
	if err != nil {
		return nil, err
	}
	return &HTTP{
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: parsed.String(),
		headers:  cfg.Headers,
	}, nil
}

func (h *HTTP) Send(message Message) error {
	if message == nil {
		return errors.New("http message is required")
	}
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encoding http message: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range h.headers {
		req.Header.Set(key, value)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending http message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("http response status %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	return nil
}
