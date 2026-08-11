package esim

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var testDownloadWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func TestDownloadSessionDisconnectCancelsContext(t *testing.T) {
	t.Parallel()

	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testDownloadWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		_ = newDownloadSession(conn, cancel)
		select {
		case <-ctx.Done():
			done <- nil
		case <-time.After(time.Second):
			done <- errors.New("websocket disconnect did not cancel download context")
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
