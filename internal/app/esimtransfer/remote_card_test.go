package esimtransfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/damonto/euicc-go/driver/iso7816"
	"github.com/gorilla/websocket"
)

func remoteCardPeer(t *testing.T, action func(context.Context, *remoteCard) error) (*websocket.Conn, <-chan error) {
	t.Helper()
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := testWSUpgrader.Upgrade(w, req, nil)
		if err != nil {
			done <- err
			return
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		session := newWSSession(ctx, conn, cancel)
		defer session.close()
		done <- action(ctx, newRemoteCard(session, "reader"))
	}))
	t.Cleanup(server.Close)
	conn := dialTestWebSocket(t, server.URL)
	t.Cleanup(func() {
		// A peer can already have closed after a protocol error.
		_ = conn.Close()
	})
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return conn, done
}

func readCardRequest(t *testing.T, conn *websocket.Conn) cardRequest {
	t.Helper()
	var message wsServerMessage
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message.Type != "card_request" || message.Card == nil {
		t.Fatalf("expected card request, got type %q", message.Type)
	}
	return *message.Card
}

func replyToCard(t *testing.T, conn *websocket.Conn, reply cardReply) {
	t.Helper()
	if err := conn.WriteJSON(wsClientMessage{Type: "card_response", Card: &reply}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCardLogicalChannel(t *testing.T) {
	command := []byte{0x01, 0xCA, 0x00, 0x00}
	conn, done := remoteCardPeer(t, func(ctx context.Context, remote *remoteCard) error {
		tx, err := remote.open(ctx)
		if err != nil {
			return err
		}
		channel := iso7816.NewChannel(tx)
		if err := channel.Connect(); err != nil {
			return err
		}
		if _, err := channel.OpenLogicalChannel([]byte{0xA0, 0x00}); err != nil {
			return err
		}
		response, err := channel.Transmit(command)
		if err != nil {
			return err
		}
		if !bytes.Equal(response, []byte{0x6A, 0x82}) {
			return errors.New("APDU status was not preserved")
		}
		if err := channel.Disconnect(); err != nil {
			return err
		}
		if err := channel.Disconnect(); err != nil {
			return err
		}
		tx, err = remote.open(ctx)
		if err != nil {
			return err
		}
		return tx.Close()
	})
	steps := []struct {
		operation         string
		command, response []byte
	}{
		{operation: "open"},
		{operation: "transmit", command: []byte("\x80\xAA\x00\x00\x0A\xA9\x08\x81\x00\x82\x01\x01\x83\x01\x07"), response: []byte{0x90, 0x00}},
		{operation: "transmit", command: []byte{0x00, 0x70, 0x00, 0x00, 0x01}, response: []byte{0x01, 0x90, 0x00}},
		{operation: "transmit", command: []byte{0x01, 0xA4, 0x04, 0x00, 0x02, 0xA0, 0x00}, response: []byte{0x90, 0x00}},
		{operation: "transmit", command: command, response: []byte{0x6A, 0x82}},
		{operation: "transmit", command: []byte{0x00, 0x70, 0x80, 0x01, 0x00}, response: []byte{0x90, 0x00}},
		{operation: "close"},
		{operation: "open"},
		{operation: "close"},
	}
	var connectionID string
	for i, step := range steps {
		request := readCardRequest(t, conn)
		if request.Operation != step.operation || !bytes.Equal(request.Command, step.command) || request.ID != fmt.Sprint(i+1) {
			t.Fatalf("request %d did not match scripted APDU operation", i)
		}
		if step.operation == "open" {
			if request.ConnectionID == connectionID {
				t.Fatal("reopened card reused connection ID")
			}
			connectionID = request.ConnectionID
		}
		if request.ConnectionID != connectionID {
			t.Fatal("card connection changed within a lease")
		}
		replyToCard(t, conn, cardReply{ID: request.ID, ConnectionID: request.ConnectionID, Data: step.response})
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCardFailureNeverReplays(t *testing.T) {
	for _, name := range []string{"wrong request", "wrong connection", "short response", "reader error", "disconnect", "timeout", "cancel"} {
		t.Run(name, func(t *testing.T) {
			conn, done := remoteCardPeer(t, func(ctx context.Context, remote *remoteCard) error {
				tx, err := remote.open(ctx)
				if err != nil {
					return err
				}
				operationCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
				if _, err := tx.Transmit(operationCtx, []byte{0x00, 0xA4, 0x00, 0x00}); !errors.Is(err, errSessionDisconnected) {
					return fmt.Errorf("card failure must terminate the session: %w", err)
				}
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
					return errors.New("card failure did not cancel the transfer")
				}
				if _, err := tx.Transmit(context.Background(), []byte{0x00, 0xA4, 0x00, 0x00}); err == nil {
					return errors.New("poisoned transport remained usable")
				}
				if err := tx.Close(); err == nil {
					return errors.New("uncertain close reported success")
				}
				if _, err := remote.open(context.Background()); err == nil {
					return errors.New("poisoned reader reopened")
				}
				return nil
			})
			opened := readCardRequest(t, conn)
			replyToCard(t, conn, cardReply{ID: opened.ID, ConnectionID: opened.ConnectionID})
			request := readCardRequest(t, conn)
			reply := cardReply{ID: request.ID, ConnectionID: request.ConnectionID, Data: []byte{0x90, 0x00}}
			switch name {
			case "wrong request":
				reply.ID = opened.ID
			case "wrong connection":
				reply.ConnectionID = "previous"
			case "short response":
				reply.Data = []byte{0x90}
			case "reader error":
				reply.Error = "reader_interrupted"
			case "disconnect":
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				if err := conn.WriteJSON(wsClientMessage{Type: wsTypeCancel}); err != nil {
					t.Fatal(err)
				}
			}
			if name != "timeout" && name != "disconnect" && name != "cancel" {
				replyToCard(t, conn, reply)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if name != "disconnect" {
				var msg wsServerMessage
				if err := conn.ReadJSON(&msg); err == nil {
					t.Fatal("received another card request after failure")
				}
			}
		})
	}
}

func TestRemoteSourceBoundToConnection(t *testing.T) {
	for _, name := range []string{"version", "source", "reader"} {
		t.Run(name, func(t *testing.T) {
			conn, done := remoteCardPeer(t, func(ctx context.Context, remote *remoteCard) error {
				runner := &transferRunner{remote: remote}
				first := wsClientMessage{Type: "source", Version: 1, SourceType: SourceClientCCID, SourceID: "reader"}
				if name == "version" {
					first.Version = 2
				}
				_, err := runner.browseRemoteSource(ctx, first, nil)
				if !errors.Is(err, ErrSourceUnsupported) {
					return fmt.Errorf("unexpected binding error: %w", err)
				}
				return nil
			})
			if name != "version" {
				var ready wsServerMessage
				if err := conn.ReadJSON(&ready); err != nil {
					t.Fatal(err)
				}
				if ready.Type != "source_ready" || ready.Version != 1 {
					t.Fatal("missing version handshake")
				}
				msg := wsClientMessage{Type: wsTypeStart, SourceType: SourceClientCCID, SourceID: "reader"}
				if name == "source" {
					msg.SourceType = SourceCCID
				} else {
					msg.SourceID = "other"
				}
				if err := conn.WriteJSON(msg); err != nil {
					t.Fatal(err)
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := (&transferRunner{}).openReader(t.Context(), startRequest{SourceType: SourceClientCCID, SourceID: "reader"}); !errors.Is(err, ErrSourceUnsupported) {
		t.Fatalf("unbound client reader error = %v", err)
	}
}
