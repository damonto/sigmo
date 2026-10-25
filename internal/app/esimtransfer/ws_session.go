package esimtransfer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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
	readerWG       sync.WaitGroup
	writeMu        sync.Mutex
	cardMu         sync.Mutex
	cardPending    *cardRequest
	cardReplies    chan cardReply
	conn           *websocket.Conn
	disconnectCh   chan struct{}
	disconnectOnce sync.Once
	startCh        chan wsClientMessage
	inputCh        chan wsClientMessage
	deleteCh       chan wsClientMessage
}

func newWSSession(ctx context.Context, conn *websocket.Conn, cancel context.CancelFunc) *wsSession {
	session := &wsSession{
		conn:         conn,
		disconnectCh: make(chan struct{}),
		startCh:      make(chan wsClientMessage, 1),
		inputCh:      make(chan wsClientMessage, 1),
		deleteCh:     make(chan wsClientMessage, 1),
		cardReplies:  make(chan cardReply, 1),
	}
	conn.SetReadLimit(128 << 10)
	session.readerWG.Go(func() {
		stop := context.AfterFunc(ctx, session.disconnect)
		defer stop()
		session.readLoop(cancel)
	})
	return session
}

func (s *wsSession) close() {
	s.disconnect()
	s.readerWG.Wait()
}

func (s *wsSession) disconnect() {
	s.disconnectOnce.Do(func() {
		close(s.disconnectCh)
		// Wake the reader and cancel the transfer even when the peer is silent.
		if s.conn != nil {
			_ = s.conn.Close() // The connection is terminal; close errors add no recovery path.
		}
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
		case wsTypeStart, "source", "profiles":
			select {
			case s.startCh <- msg:
			default:
				return
			}
		case "card_response":
			if !s.acceptCardReply(msg.Card) {
				return
			}
		case "card_invalidated":
			return
		case wsTypeUserInput:
			sendLatest(s.inputCh, msg)
		case wsTypeSourceDeletion:
			sendLatest(s.deleteCh, msg)
		case wsTypeCancel:
			cancel()
			return
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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	select {
	case <-s.disconnectCh:
		return errSessionDisconnected
	default:
	}
	if err := s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		s.disconnect()
		return fmt.Errorf("set transfer write deadline: %w", errors.Join(errSessionDisconnected, err))
	}
	if err := s.conn.WriteJSON(msg); err != nil {
		s.disconnect()
		return fmt.Errorf("send transfer message: %w", errors.Join(errSessionDisconnected, err))
	}
	return nil
}

func (s *wsSession) acceptCardReply(reply *cardReply) bool {
	s.cardMu.Lock()
	defer s.cardMu.Unlock()
	if reply == nil || s.cardPending == nil || reply.ID != s.cardPending.ID || reply.ConnectionID != s.cardPending.ConnectionID {
		return false
	}
	s.cardPending = nil
	select {
	case s.cardReplies <- *reply:
		return true
	default:
		return false
	}
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
