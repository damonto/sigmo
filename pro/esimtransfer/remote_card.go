//go:build esim_transfer

package esimtransfer

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const remoteCardVersion = 1
const remoteCardTimeout = 30 * time.Second
const maxCommandLength = 65544
const maxResponseLength = 65538

type cardRequest struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connectionId"`
	Operation    string `json:"operation"`
	Command      []byte `json:"command,omitempty"`
}

type cardReply struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connectionId"`
	Data         []byte `json:"data,omitempty"`
	Error        string `json:"error,omitempty"`
}

// remoteCard is scoped to one authenticated WebSocket and one selected reader.
// Transport failures poison it because an APDU may already have executed.
type remoteCard struct {
	session  *wsSession
	sourceID string
	lockKey  string
	mu       sync.Mutex
	nextID   uint64
	activeID string
}

func newRemoteCard(session *wsSession, sourceID string) *remoteCard {
	return &remoteCard{session: session, sourceID: sourceID, lockKey: "client_ccid:" + rand.Text()}
}

// exchange requires mu. A reply belongs to exactly one in-flight request.
func (r *remoteCard) exchange(ctx context.Context, request cardRequest) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteCardTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-r.session.disconnectCh:
		return nil, errSessionDisconnected
	default:
	}
	r.nextID++
	request.ID = strconv.FormatUint(r.nextID, 10)
	r.session.cardMu.Lock()
	r.session.cardPending = &request
	r.session.cardMu.Unlock()
	defer func() {
		r.session.cardMu.Lock()
		r.session.cardPending = nil
		r.session.cardMu.Unlock()
	}()
	if err := r.session.send(wsServerMessage{Type: "card_request", Card: &request}); err != nil {
		return nil, r.invalidate(err)
	}
	select {
	case reply := <-r.session.cardReplies:
		if ctx.Err() != nil {
			return nil, r.invalidate(ctx.Err())
		}
		select {
		case <-r.session.disconnectCh:
			return nil, r.invalidate(errSessionDisconnected)
		default:
		}
		if reply.Error != "" {
			// Never include peer-supplied APDU data or error details in logs.
			return nil, r.invalidate(errors.New("client reader operation interrupted"))
		}
		if len(reply.Data) > maxResponseLength || (request.Operation == "transmit" && len(reply.Data) < 2) ||
			(request.Operation != "transmit" && len(reply.Data) != 0) {
			return nil, r.invalidate(errors.New("invalid remote card response"))
		}
		return reply.Data, nil
	case <-ctx.Done():
		return nil, r.invalidate(ctx.Err())
	case <-r.session.disconnectCh:
		return nil, r.invalidate(errSessionDisconnected)
	}
}

func (r *remoteCard) invalidate(err error) error {
	r.session.disconnect()
	return fmt.Errorf("remote card result is uncertain; check card state before retrying: %w", errors.Join(errSessionDisconnected, err))
}

func (r *remoteCard) open(ctx context.Context) (*remoteCardTransport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeID != "" {
		return nil, errors.New("remote card is already open")
	}
	id := strconv.FormatUint(r.nextID+1, 10)
	if _, err := r.exchange(ctx, cardRequest{Operation: "open", ConnectionID: id}); err != nil {
		return nil, err
	}
	r.activeID = id
	return &remoteCardTransport{card: r, id: id}, nil
}

type remoteCardTransport struct {
	card   *remoteCard
	id     string
	closed bool
}

func (t *remoteCardTransport) Transmit(ctx context.Context, command []byte) ([]byte, error) {
	t.card.mu.Lock()
	defer t.card.mu.Unlock()
	if t.closed || t.card.activeID != t.id {
		return nil, errors.New("remote card transport is closed")
	}
	if len(command) < 4 || len(command) > maxCommandLength {
		return nil, errors.New("invalid remote APDU command length")
	}
	return t.card.exchange(ctx, cardRequest{Operation: "transmit", ConnectionID: t.id, Command: command})
}

func (t *remoteCardTransport) Close() error {
	t.card.mu.Lock()
	defer t.card.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	if t.card.activeID != t.id {
		return errors.New("remote card connection changed")
	}
	_, err := t.card.exchange(context.Background(), cardRequest{Operation: "close", ConnectionID: t.id})
	t.card.activeID = ""
	return err
}

func (s *transferRunner) browseRemoteSource(ctx context.Context, first wsClientMessage, target *mmodem.Modem) (wsClientMessage, error) {
	if first.Version != remoteCardVersion || first.SourceType != SourceClientCCID || first.SourceID == "" {
		return wsClientMessage{}, ErrSourceUnsupported
	}
	session := s.remote.session
	if err := session.send(wsServerMessage{Type: "source_ready", Version: remoteCardVersion}); err != nil {
		return wsClientMessage{}, err
	}
	for {
		msg, err := session.waitMessage(ctx, session.startCh)
		if err != nil {
			return wsClientMessage{}, err
		}
		if msg.SourceType != SourceClientCCID || msg.SourceID != s.remote.sourceID {
			return wsClientMessage{}, ErrSourceUnsupported
		}
		switch msg.Type {
		case wsTypeStart:
			return msg, nil
		case "profiles":
			profiles, err := s.Profiles(ctx, target, ProfilesRequest{
				SourceType: SourceClientCCID, SourceID: msg.SourceID, SourceIMEI: msg.SourceIMEI,
			})
			if err != nil {
				return wsClientMessage{}, err
			}
			if err := session.send(wsServerMessage{Type: "profiles", Profiles: profiles}); err != nil {
				return wsClientMessage{}, err
			}
		default:
			return wsClientMessage{}, errors.New("unexpected remote card session message")
		}
	}
}
