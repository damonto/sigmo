//go:build esim_transfer

package esimtransfer

import (
	"context"
	"sync"

	"github.com/gorilla/websocket"
)

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

type session struct {
	conn           *websocket.Conn
	disconnectCh   chan struct{}
	disconnectOnce sync.Once
	startCh        chan clientMessage
	inputCh        chan clientMessage
	deleteCh       chan clientMessage
}

func newSession(conn *websocket.Conn, cancel context.CancelFunc) *session {
	session := &session{
		conn:         conn,
		disconnectCh: make(chan struct{}),
		startCh:      make(chan clientMessage, 1),
		inputCh:      make(chan clientMessage, 1),
		deleteCh:     make(chan clientMessage, 1),
	}
	go session.readLoop(cancel)
	return session
}

func (s *session) disconnect() {
	s.disconnectOnce.Do(func() {
		close(s.disconnectCh)
	})
}

func (s *session) readLoop(cancel context.CancelFunc) {
	defer s.disconnect()
	for {
		var msg clientMessage
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

func sendLatest(ch chan clientMessage, msg clientMessage) {
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

func (s *session) send(msg serverMessage) error {
	if err := s.conn.WriteJSON(msg); err != nil {
		s.disconnect()
		return err
	}
	return nil
}

func (s *session) sendIfConnected(msg serverMessage) {
	select {
	case <-s.disconnectCh:
		return
	default:
	}
	_ = s.send(msg)
}

func (s *session) waitForStart(ctx context.Context) (clientMessage, bool) {
	select {
	case msg := <-s.startCh:
		return msg, true
	case <-ctx.Done():
		return clientMessage{}, false
	case <-s.disconnectCh:
		return clientMessage{}, false
	}
}

func (s *session) waitForUserInput(ctx context.Context) (clientMessage, bool) {
	select {
	case msg := <-s.inputCh:
		return msg, true
	case <-ctx.Done():
		return clientMessage{}, false
	case <-s.disconnectCh:
		return clientMessage{}, false
	}
}

func (s *session) waitForSourceDeletion(ctx context.Context) (clientMessage, bool) {
	select {
	case msg := <-s.deleteCh:
		return msg, true
	case <-ctx.Done():
		return clientMessage{}, false
	case <-s.disconnectCh:
		return clientMessage{}, false
	}
}
