//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

var errSessionDisconnected = errors.New("transfer session disconnected")

const (
	wsTypeProgress       = "progress"
	wsTypePreview        = "preview"
	wsTypeCancel         = "cancel"
	wsTypeStart          = "start"
	wsTypeUserInput      = "user_input"
	wsTypeSourceDeletion = "source_deletion"
	wsTypeWebsheet       = "websheet"
	wsTypeCompleted      = "completed"
	wsTypeError          = "error"
)

type wsSession struct {
	conn           *websocket.Conn
	disconnectCh   chan struct{}
	disconnectOnce sync.Once
	startCh        chan wsClientMessage
	inputCh        chan wsClientMessage
	deleteCh       chan wsClientMessage
}

func newWSSession(conn *websocket.Conn, cancel context.CancelFunc) *wsSession {
	session := &wsSession{
		conn:         conn,
		disconnectCh: make(chan struct{}),
		startCh:      make(chan wsClientMessage, 1),
		inputCh:      make(chan wsClientMessage, 1),
		deleteCh:     make(chan wsClientMessage, 1),
	}
	go session.readLoop(cancel)
	return session
}

func (s *wsSession) disconnect() {
	s.disconnectOnce.Do(func() {
		close(s.disconnectCh)
	})
}

func (s *wsSession) readLoop(cancel context.CancelFunc) {
	defer func() {
		cancel()
		s.disconnect()
	}()
	for {
		var msg wsClientMessage
		if err := s.conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case wsTypeStart:
			sendLatest(s.startCh, msg)
		case wsTypeUserInput:
			sendLatest(s.inputCh, msg)
		case wsTypeSourceDeletion:
			sendLatest(s.deleteCh, msg)
		case wsTypeCancel:
			cancel()
		}
	}
}

func sendLatest(ch chan wsClientMessage, msg wsClientMessage) {
	select {
	case ch <- msg:
	default:
		select {
		case <-ch:
		default:
		}
		ch <- msg
	}
}

func (s *wsSession) send(msg wsServerMessage) error {
	if err := s.conn.WriteJSON(msg); err != nil {
		s.disconnect()
		return fmt.Errorf("send transfer message: %w", errors.Join(errSessionDisconnected, err))
	}
	return nil
}

func (s *wsSession) sendIfConnected(msg wsServerMessage) {
	select {
	case <-s.disconnectCh:
		return
	default:
	}
	// Progress is best effort; send marks the session disconnected on failure.
	_ = s.send(msg)
}

func (s *wsSession) waitMessage(ctx context.Context, messages <-chan wsClientMessage) (wsClientMessage, error) {
	if err := ctx.Err(); err != nil {
		return wsClientMessage{}, err
	}
	// Ignore queued replies once the session is already disconnected.
	select {
	case <-s.disconnectCh:
		return wsClientMessage{}, errSessionDisconnected
	default:
	}
	select {
	case msg := <-messages:
		return msg, nil
	case <-ctx.Done():
		return wsClientMessage{}, ctx.Err()
	case <-s.disconnectCh:
		return wsClientMessage{}, errSessionDisconnected
	}
}
