package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

const defaultEndpoint = "https://api.telegram.org"

type Sender struct {
	client         *http.Client
	sendMessageURL string
	recipients     []int64
}

func New(channel *settings.Channel) (*Sender, error) {
	if strings.TrimSpace(channel.BotToken) == "" {
		return nil, errors.New("telegram bot token is required")
	}
	endpoint := strings.TrimSpace(channel.Endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	baseURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing telegram endpoint: %w", err)
	}
	recipients, err := channel.Recipients.Int64s()
	if err != nil {
		return nil, fmt.Errorf("parsing telegram recipients: %w", err)
	}
	if len(recipients) == 0 {
		return nil, errors.New("telegram recipients are required")
	}

	sendMessageURL := *baseURL
	sendMessageURL.Path = path.Join(sendMessageURL.Path, "bot"+channel.BotToken, "sendMessage")
	return &Sender{
		client:         &http.Client{Timeout: 10 * time.Second},
		sendMessageURL: sendMessageURL.String(),
		recipients:     recipients,
	}, nil
}

func (s *Sender) Send(ctx context.Context, msg notifycontent.Message) error {
	if len(s.recipients) == 0 {
		return errors.New("telegram recipients are required")
	}
	content := render(msg)

	var combined error
	for _, recipient := range s.recipients {
		if err := s.sendOne(ctx, recipient, content); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}
