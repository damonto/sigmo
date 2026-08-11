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
		return err
	}
	return nil
}

func (s *wsSession) sendIfConnected(msg wsServerMessage) {
	select {
	case <-s.disconnectCh:
		return
	default:
	}
	_ = s.send(msg)
}

func (s *wsSession) waitForStart(ctx context.Context) (wsClientMessage, bool) {
	select {
	case msg := <-s.startCh:
		return msg, true
	case <-ctx.Done():
		return wsClientMessage{}, false
	case <-s.disconnectCh:
		return wsClientMessage{}, false
	}
}

func (s *wsSession) waitForUserInput(ctx context.Context) (wsClientMessage, bool) {
	select {
	case msg := <-s.inputCh:
		return msg, true
	case <-ctx.Done():
		return wsClientMessage{}, false
	case <-s.disconnectCh:
		return wsClientMessage{}, false
	}
}

func (s *wsSession) waitForSourceDeletion(ctx context.Context) (wsClientMessage, bool) {
	select {
	case msg := <-s.deleteCh:
		return msg, true
	case <-ctx.Done():
		return wsClientMessage{}, false
	case <-s.disconnectCh:
		return wsClientMessage{}, false
	}
}
