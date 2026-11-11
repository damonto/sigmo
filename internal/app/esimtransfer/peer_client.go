package esimtransfer

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/ts43-go"
)

type peerClient struct {
	wire         *peerWire
	gate         chan struct{}
	nextID       uint64
	progress     chan string
	stopProgress chan struct{}
	stopOnce     sync.Once
	progressWG   sync.WaitGroup
	expectedEID  string
	device       ts43.Device
	option       ProfileResponse
	target       peerTarget
}

func newPeerClient(wire *peerWire) *peerClient {
	return &peerClient{wire: wire, gate: make(chan struct{}, 1), progress: make(chan string, 1), stopProgress: make(chan struct{})}
}

func (p *peerClient) exchange(ctx context.Context, message peerMessage) (peerMessage, error) {
	// Waiting for the current RPC is cancellable too. An expired waiter must not
	// transmit a new command when another operation finally releases the channel.
	select {
	case p.gate <- struct{}{}:
	case <-ctx.Done():
		return peerMessage{}, ctx.Err()
	case <-p.wire.ctx.Done():
		return peerMessage{}, p.wire.ctx.Err()
	}
	defer func() { <-p.gate }()
	if err := ctx.Err(); err != nil {
		return peerMessage{}, err
	}
	// Once a command can be sent, cancellation makes its result uncertain and
	// closes the transport immediately, including an in-flight blocked write.
	stop := context.AfterFunc(ctx, p.wire.cancel)
	defer stop()
	p.nextID++
	message.ID = p.nextID
	if err := p.wire.send(message); err != nil {
		if ctx.Err() != nil {
			return peerMessage{}, ctx.Err()
		}
		return peerMessage{}, err
	}
	reply, err := p.wire.receive(ctx)
	if err != nil {
		p.wire.cancel()
		return peerMessage{}, err
	}
	if reply.Type != "reply" || reply.ID != message.ID {
		p.wire.cancel()
		return peerMessage{}, errors.New("invalid remote transfer response")
	}
	return reply, nil
}

// Progress is coalesced separately from card operations. A UI progress callback
// never waits for a network reply while holding the transfer socket's writer.
func (p *peerClient) reportProgress(stage string) {
	select {
	case <-p.stopProgress:
		return
	default:
	}
	select {
	case p.progress <- stage:
		return
	default:
	}
	select {
	case <-p.progress:
	default:
	}
	select {
	case p.progress <- stage:
	default:
	}
}

func (p *peerClient) startProgress() {
	p.progressWG.Go(func() {
		for {
			select {
			case <-p.stopProgress:
				return
			case <-p.wire.ctx.Done():
				return
			default:
			}
			select {
			case <-p.stopProgress:
				return
			case <-p.wire.ctx.Done():
				return
			case stage := <-p.progress:
				if _, err := p.operation(p.wire.ctx, peerMessage{Type: "progress", Stage: stage}); err != nil {
					return
				}
			}
		}
	})
}

func (p *peerClient) endProgress() {
	p.stopOnce.Do(func() { close(p.stopProgress) })
	p.progressWG.Wait()
}

func (p *peerClient) close() {
	p.wire.cancel()
	p.endProgress()
	p.wire.close()
}

func (p *peerClient) operation(ctx context.Context, message peerMessage) (peerMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return p.exchange(ctx, message)
}

func (p *peerClient) Identity(ctx context.Context) (*ts43.Identity, error) {
	reply, err := p.operation(ctx, peerMessage{Type: "identity"})
	if err != nil {
		return nil, err
	}
	if reply.Identity == nil || reply.Identity.ICCID != p.option.ICCID {
		p.wire.cancel()
		return nil, errors.New("remote source profile changed")
	}
	return reply.Identity.identity(), nil
}
func (p *peerClient) AuthenticateAKA(ctx context.Context, request *ts43.AKARequest) (*ts43.AKAResponse, error) {
	if request == nil {
		return nil, errors.New("AKA challenge is required")
	}
	reply, err := p.operation(ctx, peerMessage{Type: "aka", AKA: &peerAKARequest{Rand: request.Rand, Autn: request.Autn}})
	if err != nil {
		return nil, err
	}
	if reply.AKAResponse == nil {
		p.wire.cancel()
		return nil, errors.New("invalid remote authentication response")
	}
	return reply.AKAResponse.response(), nil
}
func (p *peerClient) delete(ctx context.Context, iccid string) error {
	if iccid != p.option.ICCID {
		return errors.New("source deletion is outside the approved transfer")
	}
	_, err := p.operation(ctx, peerMessage{Type: "delete", ICCID: iccid})
	return err
}
func (p *peerClient) closeSource() {
	ctx, cancel := context.WithTimeout(p.wire.ctx, 30*time.Second)
	defer cancel()
	// Teardown never replays an operation whose response may have been lost.
	_, _ = p.operation(ctx, peerMessage{Type: "close_source"})
}
func (p *peerClient) finish(ctx context.Context, outcome string) error {
	p.endProgress()
	_, err := p.operation(ctx, peerMessage{Type: "finish", Outcome: outcome})
	return err
}

func (s *transferRunner) peerTarget(ctx context.Context, target *mmodem.Modem, seID string) (peerTarget, error) {
	imei, err := target.ThreeGPP().IMEI(ctx)
	if err != nil {
		return peerTarget{}, err
	}
	lease, err := s.lpaClients.Acquire(ctx, target, seID)
	if err != nil {
		return peerTarget{}, err
	}
	eid, readErr := lease.EID()
	if err := errors.Join(readErr, lease.Close()); err != nil {
		return peerTarget{}, err
	}
	return peerTarget{Name: modemName(s.settingsSnapshot(), target), IMEI: imei, EID: strings.ToUpper(hex.EncodeToString(eid))}, nil
}

func (s *transferRunner) requestSigmoSource(ctx context.Context, session *wsSession, first wsClientMessage, target *mmodem.Modem) (start wsClientMessage, client *peerClient, result error) {
	if first.Version != peerVersion || first.SEID == "" || first.SourceURL == "" || first.SourceID != "" || first.ProfileID != "" || first.SourceIMEI != "" {
		return start, nil, errors.New("invalid remote transfer request")
	}
	targetInfo, err := s.peerTarget(ctx, target, first.SEID)
	if err != nil {
		return start, nil, err
	}
	session.sendIfConnected(wsServerMessage{Type: "progress", Stage: "waiting_authorization"})
	conn, err := dialPeer(ctx, first.SourceURL)
	if err != nil {
		return start, nil, err
	}
	wire := newPeerWire(ctx, conn, s.allowed)
	client = newPeerClient(wire)
	client.target = targetInfo
	sourceStarted := false
	defer func() {
		if result != nil {
			wire.close()
			if sourceStarted {
				result = errors.Join(errPeerResultUncertain, result)
			}
		}
	}()
	// Losing the peer must also stop the UI wait and any target LPA operation.
	if err := wire.send(peerMessage{Type: "authorize", Version: peerVersion, Target: &targetInfo}); err != nil {
		return start, nil, err
	}
	reply, err := receivePeerWithin(wire, 15*time.Second)
	if err != nil {
		return start, nil, err
	}
	if reply.Type != "pending" || reply.Version != peerVersion {
		return start, nil, errPeerUnsupported
	}
	reply, err = receivePeerWithin(wire, peerWaitTimeout+30*time.Second)
	if err != nil {
		return start, nil, err
	}
	if reply.Type != "approved" || reply.Version != peerVersion || reply.Profile == nil ||
		reply.Profile.Type != ProfileESIM || !reply.Profile.Supported || reply.Profile.ID == "" || reply.Profile.ICCID == "" || len(reply.SourceEID) != 32 {
		return start, nil, errors.New("invalid remote transfer approval")
	}
	client.option, client.expectedEID = *reply.Profile, reply.SourceEID
	start = first
	start.Type, start.SourceID, start.ProfileID = "start", "sigmo", client.option.ID
	actual, err := s.peerTarget(wire.ctx, target, first.SEID)
	if err != nil {
		return start, nil, err
	}
	if actual.IMEI != targetInfo.IMEI || actual.EID != targetInfo.EID {
		return start, nil, errors.New("remote transfer target changed")
	}
	if err := session.send(wsServerMessage{Type: "source_selected", Version: peerVersion, SourceProfile: &client.option, SourceName: reply.SourceName}); err != nil {
		return start, nil, err
	}
	sourceStarted = true
	if err := wire.send(peerMessage{Type: "prepare"}); err != nil {
		return start, nil, err
	}
	reply, err = receivePeerWithin(wire, 45*time.Second)
	if err != nil {
		return start, nil, err
	}
	if reply.Type != "ready" || reply.Device == nil || reply.Device.ICCID != client.option.ICCID || reply.Device.EID != client.expectedEID {
		return start, nil, errors.New("invalid remote source identity")
	}
	client.device = reply.Device.device()
	return start, client, nil
}
