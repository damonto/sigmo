package telegram

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestSendOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) (*Sender, context.Context)
		wantErr error
	}{
		{
			name: "preserves canceled context",
			setup: func(t *testing.T) (*Sender, context.Context) {
				t.Helper()

				server := httptest.NewServer(nil)
				t.Cleanup(server.Close)

				ctx, cancel := context.WithCancel(t.Context())
				cancel()

				return &Sender{
					client:         server.Client(),
					sendMessageURL: server.URL,
				}, ctx
			},
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sender, ctx := tt.setup(t)
			err := sender.sendOne(ctx, 123456, content{
				Text:      "hello",
				ParseMode: parseModeMarkdownV2,
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("sendOne() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
