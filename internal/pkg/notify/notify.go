package notify

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/damonto/sigmo/internal/pkg/config"
)

type Message interface {
	fmt.Stringer
	Markdown() string
}

type Sender interface {
	Send(message Message) error
}

type SenderFunc func(message Message) error

func (f SenderFunc) Send(message Message) error {
	return f(message)
}

// Notifier manages multiple notification channels.
type Notifier struct {
	channels map[string]Sender
	cfg      *config.Config
}

// New creates a new Notifier from the given configuration.
func New(cfg *config.Config) (*Notifier, error) {
	if cfg == nil || len(cfg.Channels) == 0 {
		return &Notifier{
			channels: make(map[string]Sender),
			cfg:      cfg,
		}, nil
	}

	channels := make(map[string]Sender)
	for name, channel := range cfg.Channels {
		channelName := strings.ToLower(name)
		sender, err := createSender(channelName, channel)
		if err != nil {
			return nil, fmt.Errorf("creating %s channel: %w", name, err)
		}
		channels[channelName] = sender
	}

	return &Notifier{channels: channels, cfg: cfg}, nil
}

func createSender(name string, channel config.Channel) (Sender, error) {
	switch name {
	case "telegram":
		return NewTelegram(&channel)
	case "http":
		return NewHTTP(&channel)
	case "email":
		return NewEmail(&channel)
	case "bark":
		return NewBark(&channel)
	case "gotify":
		return NewGotify(&channel)
	case "sc3":
		return NewSC3(&channel)
	default:
		return nil, fmt.Errorf("unsupported channel type: %s", name)
	}
}

// Send sends a message to the specified channels.
// If no channels are specified, the message will be sent to all configured channels.
func (n *Notifier) Send(message Message, channels ...string) error {
	var targets []string
	if len(channels) == 0 {
		// Send to all configured channels
		for name := range n.cfg.Channels {
			targets = append(targets, strings.ToLower(name))
		}
	} else {
		// Send to specified channels only
		for _, name := range channels {
			channelName := strings.ToLower(name)
			if _, exists := n.channels[channelName]; !exists {
				slog.Warn("channel not found", "channel", channelName)
				continue
			}
			targets = append(targets, channelName)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	var combined error
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, target := range targets {
		sender := n.channels[target]
		wg.Add(1)
		go func(target string, sender Sender) {
			defer wg.Done()
			if err := sender.Send(message); err != nil {
				mu.Lock()
				combined = errors.Join(combined, fmt.Errorf("%s send failed: %w", target, err))
				mu.Unlock()
			}
		}(target, sender)
	}
	wg.Wait()
	return combined
}

// SendTo sends a message to a specific sender.
// Use this when you need to send to a single, manually created sender.
func SendTo(sender Sender, message Message) error {
	if sender == nil {
		return errors.New("notify sender is required")
	}
	return sender.Send(message)
}
