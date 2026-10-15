package esim

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const downloadMessageLimit = 64 << 10

var errDownloadDisconnected = errors.New("download session disconnected")

type downloadSession struct {
	readerWG           sync.WaitGroup
	writeMu            sync.Mutex
	conn               *websocket.Conn
	disconnectCh       chan struct{}
	disconnectOnce     sync.Once
	confirmCh          chan bool
	confirmationCodeCh chan string
}

func newDownloadSession(ctx context.Context, conn *websocket.Conn, cancel context.CancelFunc) *downloadSession {
	session := &downloadSession{
		conn:               conn,
		disconnectCh:       make(chan struct{}),
		confirmCh:          make(chan bool, 1),
		confirmationCodeCh: make(chan string, 1),
	}
	conn.SetReadLimit(downloadMessageLimit)
	session.readerWG.Go(func() {
		stop := context.AfterFunc(ctx, session.disconnect)
		defer stop()
		session.readLoop(cancel)
	})
	return session
}

func (s *downloadSession) close() {
	s.disconnect()
	s.readerWG.Wait()
}

func (s *downloadSession) disconnect() {
	s.disconnectOnce.Do(func() {
		close(s.disconnectCh)
		if s.conn != nil {
			_ = s.conn.Close() // Wake blocked reads and writes; the connection is terminal.
		}
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
			return
		}
	}
}

func (s *downloadSession) send(msg downloadServerMessage) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	select {
	case <-s.disconnectCh:
		return errDownloadDisconnected
	default:
	}
	if err := s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		s.disconnect()
		return fmt.Errorf("set download write deadline: %w", err)
	}
	if err := s.conn.WriteJSON(msg); err != nil {
		s.disconnect()
		return fmt.Errorf("send download message: %w", err)
	}
	return nil
}

func (s *downloadSession) sendIfConnected(msg downloadServerMessage) {
	select {
	case <-s.disconnectCh:
		return
	default:
	}
	_ = s.send(msg) // Progress is best effort; a write failure closes the session.
}

func (s *downloadSession) waitForConfirm(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case <-s.disconnectCh:
		return false
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
	if ctx.Err() != nil {
		return ""
	}
	select {
	case <-s.disconnectCh:
		return ""
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
