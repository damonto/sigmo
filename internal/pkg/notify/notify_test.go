package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/config"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	notifywebhook "github.com/damonto/sigmo/internal/pkg/notify/webhook"
)

type senderFunc func(ctx context.Context, event notifyevent.Event) error

func (f senderFunc) Send(ctx context.Context, event notifyevent.Event) error {
	return f(ctx, event)
}

func TestNotifierSend(t *testing.T) {
	t.Parallel()

	t.Run("sends to all configured channels when none are specified", func(t *testing.T) {
		t.Parallel()

		var (
			mu     sync.Mutex
			called []string
		)
		notifier := &Notifier{
			channels: map[string]Sender{
				"email": senderFunc(func(ctx context.Context, event notifyevent.Event) error {
					mu.Lock()
					called = append(called, "email")
					mu.Unlock()
					return nil
				}),
				"telegram": senderFunc(func(ctx context.Context, event notifyevent.Event) error {
					mu.Lock()
					called = append(called, "telegram")
					mu.Unlock()
					return nil
				}),
			},
		}

		if err := notifier.Send(context.Background(), notifyevent.OTPEvent{Code: "123456"}); err != nil {
			t.Fatalf("Send() error = %v", err)
		}

		slices.Sort(called)
		want := []string{"email", "telegram"}
		if !slices.Equal(called, want) {
			t.Fatalf("Send() called = %v, want %v", called, want)
		}
	})

	t.Run("sends only requested channels and ignores missing ones", func(t *testing.T) {
		t.Parallel()

		var called []string
		notifier := &Notifier{
			channels: map[string]Sender{
				"email": senderFunc(func(ctx context.Context, event notifyevent.Event) error {
					called = append(called, "email")
					return nil
				}),
			},
		}

		if err := notifier.Send(context.Background(), notifyevent.OTPEvent{Code: "123456"}, "email", "missing"); err != nil {
			t.Fatalf("Send() error = %v", err)
		}
		if !slices.Equal(called, []string{"email"}) {
			t.Fatalf("Send() called = %v, want [email]", called)
		}
	})

	t.Run("joins sender errors", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("boom")
		notifier := &Notifier{
			channels: map[string]Sender{
				"email": senderFunc(func(ctx context.Context, event notifyevent.Event) error {
					return wantErr
				}),
			},
		}

		err := notifier.Send(context.Background(), notifyevent.OTPEvent{Code: "123456"})
		if err == nil {
			t.Fatal("Send() error = nil, want joined error")
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("Send() error = %v, want wrapped %v", err, wantErr)
		}
		if !strings.Contains(err.Error(), "email send failed") {
			t.Fatalf("Send() error = %v, want channel context", err)
		}
	})

	t.Run("propagates context cancellation", func(t *testing.T) {
		t.Parallel()

		notifier := &Notifier{
			channels: map[string]Sender{
				"email": senderFunc(func(ctx context.Context, event notifyevent.Event) error {
					return ctx.Err()
				}),
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := notifier.Send(ctx, notifyevent.OTPEvent{Code: "123456"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Send() error = %v, want %v", err, context.Canceled)
		}
	})
}

func TestNewSkipsDisabledChannels(t *testing.T) {
	t.Parallel()

	enabled := false
	notifier, err := New(&config.Config{
		Channels: map[string]config.Channel{
			"telegram": {
				Enabled:  &enabled,
				BotToken: "draft-token",
			},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if len(notifier.channels) != 0 {
		t.Fatalf("channels = %d, want 0", len(notifier.channels))
	}
}

func TestHTTPSend(t *testing.T) {
	t.Parallel()

	type payload struct {
		Kind    notifyevent.Kind     `json:"kind"`
		Payload notifyevent.OTPEvent `json:"payload"`
	}

	var (
		got     payload
		gotAuth string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender, err := notifywebhook.New(&config.Channel{
		Endpoint: server.URL,
		Headers: map[string]string{
			"Authorization": "Bearer secret",
		},
	})
	if err != nil {
		t.Fatalf("NewHTTP() error = %v", err)
	}

	if err := sender.Send(context.Background(), notifyevent.OTPEvent{Code: "654321"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got.Kind != notifyevent.KindOTP {
		t.Fatalf("kind = %q, want %q", got.Kind, notifyevent.KindOTP)
	}
	if got.Payload.Code != "654321" {
		t.Fatalf("payload.code = %q, want %q", got.Payload.Code, "654321")
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer secret")
	}
}
