package esim

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
		session := newDownloadSession(ctx, conn, cancel)
		defer session.close()
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

func TestDownloadSessionIgnoresRepliesAfterCancellation(t *testing.T) {
	for _, disconnected := range []bool{false, true} {
		name := "canceled"
		if disconnected {
			name = "disconnected"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			session := &downloadSession{
				disconnectCh: make(chan struct{}), confirmCh: make(chan bool, 1), confirmationCodeCh: make(chan string, 1),
			}
			session.confirmCh <- true
			session.confirmationCodeCh <- "123456"
			if disconnected {
				session.disconnect()
			} else {
				cancel()
			}
			if session.waitForConfirm(ctx) {
				t.Fatal("accepted a queued confirmation after cancellation")
			}
			if code := session.waitForConfirmationCode(ctx); code != "" {
				t.Fatalf("accepted a queued confirmation code after cancellation: %q", code)
			}
		})
	}
}

func TestDownloadSessionCancellationClosesIdleConnection(t *testing.T) {
	for _, fromClient := range []bool{false, true} {
		name := "context"
		if fromClient {
			name = "client"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ready := make(chan struct{})
			done := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				conn, err := testDownloadWSUpgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					close(ready)
					return
				}
				session := newDownloadSession(ctx, conn, cancel)
				defer session.close()
				close(ready)
				<-ctx.Done()
				// Cancellation must wake the reader without relying on handler cleanup.
				session.readerWG.Wait()
			}))
			defer server.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			<-ready
			if fromClient {
				if err := conn.WriteJSON(downloadClientMessage{Type: wsTypeCancel}); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				_ = conn.Close() // Let the test server finish even when the reader leaked.
				cancel()
				t.Fatal("cancellation left the download reader running")
			}
		})
	}
}

func TestDownloadSessionSerializesProgressWrites(t *testing.T) {
	const count = 20
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		conn, err := testDownloadWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		session := newDownloadSession(ctx, conn, cancel)
		defer session.close()
		var writers sync.WaitGroup
		for range count {
			writers.Go(func() {
				if err := session.send(downloadServerMessage{Type: wsTypeProgress, Stage: "download"}); err != nil {
					t.Error(err)
				}
			})
		}
		writers.Wait()
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for range count {
		var message downloadServerMessage
		if err := conn.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message.Type != wsTypeProgress {
			t.Fatalf("unexpected message: %+v", message)
		}
	}
	<-done
}

func TestDownloadStartRejectsOversizedMessage(t *testing.T) {
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testDownloadWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_, err = readStartMessage(conn)
		done <- err
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(downloadClientMessage{Type: wsTypeStart, SMDP: strings.Repeat("a", downloadMessageLimit), SEID: "se"}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, websocket.ErrReadLimit) {
		t.Fatalf("readStartMessage error = %v, want message size limit", err)
	}
}
