package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/config"
)

const defaultTelegramEndpoint = "https://api.telegram.org"
const telegramParseModeMarkdownV2 = "MarkdownV2"

type Telegram struct {
	client         *http.Client
	sendMessageURL string
	recipients     []int64
}

type telegramMessage struct {
	ChatID    int64  `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

func NewTelegram(cfg *config.Channel) (*Telegram, error) {
	if strings.TrimSpace(cfg.BotToken) == "" {
		return nil, errors.New("telegram bot token is required")
	}
	baseURL, err := parseEndpoint("telegram", cfg.Endpoint, defaultTelegramEndpoint)
	if err != nil {
		return nil, err
	}
	recipients, err := cfg.Recipients.Int64s()
	if err != nil {
		return nil, fmt.Errorf("parsing telegram recipients: %w", err)
	}
	if len(recipients) == 0 {
		return nil, errors.New("telegram recipients is required")
	}
	sendMessageURL := *baseURL
	sendMessageURL.Path = path.Join(sendMessageURL.Path, "bot"+cfg.BotToken, "sendMessage")
	return &Telegram{
		client:         &http.Client{Timeout: 10 * time.Second},
		sendMessageURL: sendMessageURL.String(),
		recipients:     recipients,
	}, nil
}

func (t *Telegram) Send(message Message) error {
	if message == nil {
		return errors.New("telegram message is required")
	}
	if len(t.recipients) == 0 {
		return errors.New("telegram recipients are required")
	}
	var combined error
	payload := message.Markdown()
	for _, recipient := range t.recipients {
		if err := t.sendOne(recipient, payload); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func (t *Telegram) sendOne(to int64, text string) error {
	message := telegramMessage{
		ChatID:    to,
		Text:      text,
		ParseMode: telegramParseModeMarkdownV2,
	}
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encoding telegram message: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, t.sendMessageURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		slog.Error("failed to send telegram message", "recipient", to, "error", err)
		return errors.New("telegram API request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("telegram response status %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	return nil
}
