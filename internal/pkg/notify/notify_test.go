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

	"github.com/damonto/sigmo/internal/pkg/locale"
	notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	notifywebhook "github.com/damonto/sigmo/internal/pkg/notify/webhook"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type senderFunc func(ctx context.Context, msg notifycontent.Message) error

func (f senderFunc) Send(ctx context.Context, msg notifycontent.Message) error {
	return f(ctx, msg)
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
				"email": senderFunc(func(ctx context.Context, msg notifycontent.Message) error {
					mu.Lock()
					called = append(called, "email")
					mu.Unlock()
					return nil
				}),
				"telegram": senderFunc(func(ctx context.Context, msg notifycontent.Message) error {
					mu.Lock()
					called = append(called, "telegram")
					mu.Unlock()
					return nil
				}),
			},
		}

		if err := notifier.Send(t.Context(), locale.English, notifyevent.OTPEvent{Code: "123456"}); err != nil {
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
				"email": senderFunc(func(ctx context.Context, msg notifycontent.Message) error {
					called = append(called, "email")
					return nil
				}),
			},
		}

		if err := notifier.Send(t.Context(), locale.English, notifyevent.OTPEvent{Code: "123456"}, "email", "missing"); err != nil {
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
				"email": senderFunc(func(ctx context.Context, msg notifycontent.Message) error {
					return wantErr
				}),
			},
		}

		err := notifier.Send(t.Context(), locale.English, notifyevent.OTPEvent{Code: "123456"})
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

	t.Run("delivers composed content", func(t *testing.T) {
		t.Parallel()

		titles := make(chan string, 1)
		notifier := &Notifier{
			channels: map[string]Sender{
				"email": senderFunc(func(ctx context.Context, msg notifycontent.Message) error {
					titles <- msg.Title()
					return nil
				}),
			},
		}

		if err := notifier.Send(t.Context(), locale.English, notifyevent.OTPEvent{Code: "123456"}); err != nil {
			t.Fatalf("Send() error = %v", err)
		}
		if got := <-titles; got != "Sigmo Login" {
			t.Fatalf("Title() = %q, want %q", got, "Sigmo Login")
		}
	})

	t.Run("rejects events it cannot compose before sending", func(t *testing.T) {
		t.Parallel()

		notifier := &Notifier{
			channels: map[string]Sender{
				"email": senderFunc(func(ctx context.Context, msg notifycontent.Message) error {
					t.Error("Send() reached the channel, want compose error first")
					return nil
				}),
			},
		}

		if err := notifier.Send(t.Context(), locale.English, nil); err == nil {
			t.Fatal("Send() error = nil, want compose error")
		}
	})

	t.Run("propagates context cancellation", func(t *testing.T) {
		t.Parallel()

		notifier := &Notifier{
			channels: map[string]Sender{
				"email": senderFunc(func(ctx context.Context, msg notifycontent.Message) error {
					return ctx.Err()
				}),
			},
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := notifier.Send(ctx, locale.English, notifyevent.OTPEvent{Code: "123456"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Send() error = %v, want %v", err, context.Canceled)
		}
	})
}

func TestNewSkipsDisabledChannels(t *testing.T) {
	t.Parallel()

	enabled := false
	notifier, err := New(&settings.Settings{
		Channels: map[string]settings.Channel{
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

	sender, err := notifywebhook.New(&settings.Channel{
		Endpoint: server.URL,
		Headers: map[string]string{
			"Authorization": "Bearer secret",
		},
	})
	if err != nil {
		t.Fatalf("NewHTTP() error = %v", err)
	}

	msg, err := notifycontent.Compose(locale.English, notifyevent.OTPEvent{Code: "654321"})
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if err := sender.Send(t.Context(), msg); err != nil {
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
