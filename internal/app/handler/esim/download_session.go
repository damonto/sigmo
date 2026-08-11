package esim

import (
	"context"
	"sync"

	"github.com/gorilla/websocket"
)

type downloadSession struct {
	conn               *websocket.Conn
	disconnectCh       chan struct{}
	disconnectOnce     sync.Once
	confirmCh          chan bool
	confirmationCodeCh chan string
}

func newDownloadSession(conn *websocket.Conn, cancel context.CancelFunc) *downloadSession {
	session := &downloadSession{
		conn:               conn,
		disconnectCh:       make(chan struct{}),
		confirmCh:          make(chan bool, 1),
		confirmationCodeCh: make(chan string, 1),
	}
	go session.readLoop(cancel)
	return session
}

func (s *downloadSession) disconnect() {
	s.disconnectOnce.Do(func() {
		close(s.disconnectCh)
	})
}

func (s *downloadSession) readLoop(cancel context.CancelFunc) {
	defer func() {
		cancel()
		s.disconnect()
	}()
	for {
		var msg downloadClientMessage
		if err := s.conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case wsTypeConfirm:
			if msg.Accept != nil {
				select {
				case s.confirmCh <- *msg.Accept:
				default:
				}
			}
		case wsTypeConfirmationCode:
			select {
			case s.confirmationCodeCh <- msg.Code:
			default:
			}
		case wsTypeCancel:
			cancel()
		}
	}
}

func (s *downloadSession) send(msg downloadServerMessage) error {
	if err := s.conn.WriteJSON(msg); err != nil {
		s.disconnect()
		return err
	}
	return nil
}

func (s *downloadSession) sendIfConnected(msg downloadServerMessage) {
	select {
	case <-s.disconnectCh:
		return
	default:
	}
	_ = s.send(msg)
}

func (s *downloadSession) waitForConfirm(ctx context.Context) bool {
	select {
	case accept := <-s.confirmCh:
		return accept
	default:
	}
	select {
	case accept := <-s.confirmCh:
		return accept
	case <-ctx.Done():
		return false
	case <-s.disconnectCh:
		return false
	}
}

func (s *downloadSession) waitForConfirmationCode(ctx context.Context) string {
	select {
	case code := <-s.confirmationCodeCh:
		return code
	default:
	}
	select {
	case code := <-s.confirmationCodeCh:
		return code
	case <-ctx.Done():
		return ""
	case <-s.disconnectCh:
		return ""
	}
}
