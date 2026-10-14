//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestSessionCancellationInterruptsBackgroundCardOperation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		session := newWSSession(ctx, conn, cancel)
		defer session.close()
		// The LPA channel API can call the bridge without the transfer context.
		_, err = newRemoteCard(session, "reader").open(context.Background())
		done <- err
	}))
	defer server.Close()
	conn := dialTestWebSocket(t, server.URL)
	defer conn.Close()
	readCardRequest(t, conn)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, errSessionDisconnected) {
			t.Fatalf("open() error = %v, want disconnected", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session cancellation left the card operation waiting for its timeout")
	}
}
