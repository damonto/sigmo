package call

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebRTCSignalingCancellationClosesIdleConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		conn, err := callWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		// No offer is sent: cancellation must unblock the idle ReadJSON call.
		if err := (&MediaHandler{}).serveWebRTCSession(ctx, conn, nil, "call"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancel()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("canceled signaling connection remained open")
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled signaling handler did not exit")
	}
}
