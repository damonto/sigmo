package esimtransfer

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func peerClientFixture(t *testing.T, respond func(*peerWire, peerMessage) error, dialers ...*websocket.Dialer) *peerClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := testWSUpgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		wire := newPeerWire(request.Context(), conn, nil)
		defer wire.close()
		for {
			message, err := wire.receive(wire.ctx)
			if err != nil {
				return
			}
			if err := respond(wire, message); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	dialer := websocket.DefaultDialer
	if len(dialers) > 0 {
		dialer = dialers[0]
	}
	conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := newPeerClient(newPeerWire(t.Context(), conn, nil))
	t.Cleanup(client.close)
	return client
}

func TestPeerClientCancelledWaiterDoesNotSend(t *testing.T) {
	received := make(chan peerMessage, 4)
	release := make(chan struct{})
	client := peerClientFixture(t, func(wire *peerWire, message peerMessage) error {
		received <- message
		if message.ID == 1 {
			select {
			case <-release:
			case <-wire.ctx.Done():
				return wire.ctx.Err()
			}
		}
		return wire.send(peerMessage{Type: "reply", ID: message.ID})
	})
	first := make(chan error, 1)
	go func() { _, err := client.operation(t.Context(), peerMessage{Type: "identity"}); first <- err }()
	select {
	case message := <-received:
		if message.ID != 1 {
			t.Fatal("incorrect first request ID")
		}
	case <-time.After(time.Second):
		t.Fatal("first request was not sent")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.operation(ctx, peerMessage{Type: "delete", ICCID: "8901"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if _, err := client.operation(t.Context(), peerMessage{Type: "identity"}); err != nil {
		t.Fatal(err)
	}
	message := <-received
	if message.Type != "identity" || message.ID != 2 {
		t.Fatalf("cancelled call was transmitted: %+v", message)
	}
}

func TestPeerClientRejectsUnmatchedReply(t *testing.T) {
	client := peerClientFixture(t, func(wire *peerWire, message peerMessage) error {
		return wire.send(peerMessage{Type: "reply", ID: message.ID + 1})
	})
	if _, err := client.operation(t.Context(), peerMessage{Type: "delete", ICCID: "8901"}); err == nil {
		t.Fatal("unmatched response acknowledged a deletion")
	}
	if client.wire.ctx.Err() == nil {
		t.Fatal("uncertain connection remained reusable")
	}
}

func TestPeerProgressIsCoalescedAndDoesNotBlockCaller(t *testing.T) {
	received := make(chan peerMessage, 4)
	release := make(chan struct{})
	client := peerClientFixture(t, func(wire *peerWire, message peerMessage) error {
		received <- message
		if message.ID == 1 {
			select {
			case <-release:
			case <-wire.ctx.Done():
				return wire.ctx.Err()
			}
		}
		return wire.send(peerMessage{Type: "reply", ID: message.ID})
	})
	client.startProgress()
	client.reportProgress("preparing")
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("progress was not sent")
	}
	reported := make(chan struct{})
	go func() {
		for range 1000 {
			client.reportProgress("downloading")
		}
		client.reportProgress("enabling")
		close(reported)
	}()
	select {
	case <-reported:
	case <-time.After(time.Second):
		t.Fatal("a progress callback blocked on the network")
	}
	close(release)
	select {
	case message := <-received:
		if message.Stage != "enabling" {
			t.Fatalf("lost latest progress: %s", message.Stage)
		}
	case <-time.After(time.Second):
		t.Fatal("latest progress was not delivered")
	}
	client.close()
}

func TestPeerTerminalStateIsPublishedOnce(t *testing.T) {
	broker := newPeerBroker(nil)
	pending, err := broker.create("local", peerTarget{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	broker.update(pending, peerPhaseEnded, peerOutcomeCompleted)
	first := broker.snapshot()[0]
	broker.update(pending, peerPhaseEnded, peerOutcomeFailed)
	broker.update(pending, peerPhaseRunning, "")
	final := broker.snapshot()[0]
	if final.Revision != first.Revision || final.Outcome != peerOutcomeCompleted || final.Phase != peerPhaseEnded {
		t.Fatal("cleanup changed a completed result")
	}
}

func TestPeerRejectsReplayedOperationBeforeDeleting(t *testing.T) {
	peer := newPeerTestSession(t)
	peer.hello(t)
	peer.approve(t, "authorization", true)
	peer.read(t, "approved")
	peer.send(t, peerMessage{Type: "prepare"})
	peer.read(t, "ready")
	peer.send(t, peerMessage{Type: "identity"})
	peer.read(t, "reply")
	peer.send(t, peerMessage{Type: "delete", ID: 1, ICCID: "8901"})
	select {
	case err := <-peer.done:
		if err == nil {
			t.Fatal("replayed ID was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("replayed session remained open")
	}
	if peer.backend.deletions.Load() != 0 {
		t.Fatal("replayed request deleted a profile")
	}
}

func TestPeerShutdownCancelsApprovalAndRejectsNewGrants(t *testing.T) {
	peer := newPeerTestSession(t)
	peer.hello(t)
	peer.phase(t, "authorization")
	if err := peer.broker.close(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-peer.done:
		if err == nil {
			t.Fatal("closed service approved a transfer")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown left approval waiting")
	}
	if peer.broker.permits() {
		t.Fatal("closed service still accepts operations")
	}
	if _, err := peer.broker.create("local", peerTarget{}, nil); !errors.Is(err, errPeerClosed) {
		t.Fatalf("new grant after shutdown: %v", err)
	}
}

func TestPeerShutdownJoinsDeviceCleanup(t *testing.T) {
	broker := newPeerBroker(nil)
	ctx, release, err := broker.begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cancelled := make(chan struct{})
	cleanup := make(chan struct{})
	cleaned := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(cancelled)
		<-cleanup
		close(cleaned)
		release()
	}()
	stopped := make(chan error, 1)
	go func() { stopped <- broker.close(t.Context()) }()
	<-cancelled
	select {
	case <-stopped:
		t.Error("shutdown returned before device cleanup")
	default:
	}
	close(cleanup)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("device cleanup was not joined")
	}
}

// This transport preserves Close's interruption contract while making data
// writes block deterministically after the WebSocket handshake.
type blockedPeerWrites struct {
	net.Conn
	block     atomic.Bool
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func (conn *blockedPeerWrites) Write(data []byte) (int, error) {
	if !conn.block.Load() {
		return conn.Conn.Write(data)
	}
	conn.startOnce.Do(func() { close(conn.started) })
	<-conn.closed
	return 0, net.ErrClosed
}
func (conn *blockedPeerWrites) Close() error {
	conn.closeOnce.Do(func() { close(conn.closed) })
	return conn.Conn.Close()
}

func TestPeerClientCancellationInterruptsBlockedWrite(t *testing.T) {
	var transport *blockedPeerWrites
	dialer := &websocket.Dialer{NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		transport = &blockedPeerWrites{Conn: conn, started: make(chan struct{}), closed: make(chan struct{})}
		return transport, nil
	}}
	client := peerClientFixture(t, func(wire *peerWire, message peerMessage) error {
		return wire.send(peerMessage{Type: "reply", ID: message.ID})
	}, dialer)
	transport.block.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.operation(ctx, peerMessage{Type: "delete", ICCID: "8901"}); done <- err }()
	<-transport.started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("write cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled write remained blocked")
	}
}

func TestPeerHandshakeCapacityDoesNotBlockAuthenticatedSubscriptions(t *testing.T) {
	broker := newPeerBroker(nil)
	for range peerMaxPerAddress {
		_, release, err := broker.beginRemote(t.Context(), "remote")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
	}
	if _, _, err := broker.beginRemote(t.Context(), "remote"); !errors.Is(err, errPeerCapacity) {
		t.Fatalf("handshake capacity: %v", err)
	}
	_, release, err := broker.begin(t.Context())
	if err != nil {
		t.Fatal("public handshake capacity blocked a UI subscription")
	}
	release()
}

func TestPeerRequesterCannotSubmitSourceSelection(t *testing.T) {
	for name, change := range map[string]func(*wsClientMessage){
		"source":      func(message *wsClientMessage) { message.SourceID = "modem:source" },
		"profile":     func(message *wsClientMessage) { message.ProfileID = "se:8901" },
		"source_imei": func(message *wsClientMessage) { message.SourceIMEI = strings.Repeat("1", 15) },
	} {
		t.Run(name, func(t *testing.T) {
			first := wsClientMessage{Type: "source", Version: peerVersion, SourceType: SourceSigmo,
				SourceURL: "https://source.example", SEID: "target-se"}
			change(&first)
			runner := &transferRunner{}
			// Invalid selections are rejected before a modem or socket is accessed.
			if _, client, err := runner.requestSigmoSource(t.Context(), nil, first, nil); err == nil || client != nil {
				t.Fatal("requester-supplied source selection was accepted")
			}
		})
	}
}

func TestPeerRequesterRejectsUnsupportedVersionsBeforeAccessingTarget(t *testing.T) {
	for name, version := range map[string]int{"missing": 0, "unsupported": 2} {
		t.Run(name, func(t *testing.T) {
			first := wsClientMessage{Type: "source", Version: version, SourceType: SourceSigmo,
				SourceURL: "https://source.example", SEID: "target-se"}
			runner := &transferRunner{}
			if _, client, err := runner.requestSigmoSource(t.Context(), nil, first, nil); err == nil || client != nil {
				t.Fatal("unsupported requester accessed target hardware")
			}
		})
	}
}
