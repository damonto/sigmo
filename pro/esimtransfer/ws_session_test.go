//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"
	"testing"
)

func TestWaitMessage(t *testing.T) {
	for _, tt := range []struct {
		name       string
		cancel     bool
		disconnect bool
		queued     bool
		wantErr    error
	}{
		{name: "reply", queued: true},
		{name: "canceled", cancel: true, wantErr: context.Canceled},
		{name: "disconnected without canceling context", disconnect: true, wantErr: errSessionDisconnected},
		{name: "canceled reply is ignored", cancel: true, queued: true, wantErr: context.Canceled},
		{name: "disconnected reply is ignored", disconnect: true, queued: true, wantErr: errSessionDisconnected},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			session := &wsSession{disconnectCh: make(chan struct{})}
			messages := make(chan wsClientMessage, 1)
			want := wsClientMessage{Type: wsTypeUserInput, Response: "answer"}
			if tt.queued {
				messages <- want
			}
			if tt.cancel {
				cancel()
			}
			if tt.disconnect {
				session.disconnect()
			}
			got, err := session.waitMessage(ctx, messages)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("waitMessage() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (got.Type != want.Type || got.Response != want.Response) {
				t.Errorf("waitMessage() = %+v, want %+v", got, want)
			}
		})
	}
}
