package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/damonto/sigmo/internal/pkg/config"
	notifybark "github.com/damonto/sigmo/internal/pkg/notify/bark"
	notifyemail "github.com/damonto/sigmo/internal/pkg/notify/email"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	notifygotify "github.com/damonto/sigmo/internal/pkg/notify/gotify"
	notifysc3 "github.com/damonto/sigmo/internal/pkg/notify/sc3"
	notifytelegram "github.com/damonto/sigmo/internal/pkg/notify/telegram"
	notifywebhook "github.com/damonto/sigmo/internal/pkg/notify/webhook"
)

type Sender interface {
	Send(ctx context.Context, event notifyevent.Event) error
}

// Notifier manages multiple notification channels.
type Notifier struct {
	channels map[string]Sender
}

// New creates a new Notifier from the given configuration.
func New(cfg *config.Config) (*Notifier, error) {
	channels := make(map[string]Sender, len(cfg.Channels))
	for name, channel := range cfg.Channels {
		if !channel.IsEnabled() {
			continue
		}
		channelName := strings.ToLower(name)
		sender, err := createSender(channelName, channel)
		if err != nil {
			return nil, fmt.Errorf("creating %s channel: %w", name, err)
		}
		channels[channelName] = sender
	}

	return &Notifier{channels: channels}, nil
}

func createSender(name string, channel config.Channel) (Sender, error) {
	switch name {
	case "telegram":
		return notifytelegram.New(&channel)
	case "http":
		return notifywebhook.New(&channel)
	case "email":
		return notifyemail.New(&channel)
	case "bark":
		return notifybark.New(&channel)
	case "gotify":
		return notifygotify.New(&channel)
	case "sc3":
		return notifysc3.New(&channel)
	default:
		return nil, fmt.Errorf("unsupported channel type: %s", name)
	}
}

// Send sends an event to the specified channels.
// If no channels are specified, the message will be sent to all configured channels.
func (n *Notifier) Send(ctx context.Context, event notifyevent.Event, channels ...string) error {
	var targets []string
	if len(channels) == 0 {
		for name := range n.channels {
			targets = append(targets, strings.ToLower(name))
		}
	} else {
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
	slices.Sort(targets)
	var combined error
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, target := range targets {
		sender := n.channels[target]
		wg.Add(1)
		go func(target string, sender Sender) {
			defer wg.Done()
			if err := sender.Send(ctx, event); err != nil {
				mu.Lock()
				combined = errors.Join(combined, fmt.Errorf("%s send failed: %w", target, err))
				mu.Unlock()
			}
		}(target, sender)
	}
	wg.Wait()
	return combined
}
