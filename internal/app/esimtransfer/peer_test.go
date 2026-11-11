package esimtransfer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/damonto/ts43-go"
	"github.com/gorilla/websocket"
)

type fakePeerBackend struct {
	authorized     atomic.Bool
	catalogReads   atomic.Int32
	preparations   atomic.Int32
	deletions      atomic.Int32
	releases       atomic.Int32
	authentication atomic.Int32
	changed        atomic.Bool
}

func fakePeerCatalog() []peerCatalogSource {
	return []peerCatalogSource{{Source: SourceResponse{Type: SourceModem, ID: "modem", Name: "Source"}, Profiles: []ProfileResponse{{ID: "se:8901", SEID: "se", ICCID: "8901", Type: ProfileESIM, Name: "Line", Supported: true}}, EIDs: map[string]string{"se:8901": strings.Repeat("1", 32)}}}
}
func (f *fakePeerBackend) peerCatalog(context.Context, string) ([]peerCatalogSource, error) {
	f.catalogReads.Add(1)
	return fakePeerCatalog(), nil
}
func (f *fakePeerBackend) peerPrepare(_ context.Context, _ peerSelection, p ProfileResponse, eid string) (*sourceConnection, error) {
	f.preparations.Add(1)
	if f.changed.Load() {
		return nil, errors.New("source eUICC changed")
	}
	return &sourceConnection{channel: f, device: ts43.Device{ICCID: p.ICCID, EID: eid}, release: func() { f.releases.Add(1) }}, nil
}
func (f *fakePeerBackend) peerDelete(context.Context, peerSelection, ProfileResponse, string) error {
	f.deletions.Add(1)
	return nil
}
func (f *fakePeerBackend) Identity(context.Context) (*ts43.Identity, error) {
	return &ts43.Identity{ICCID: "8901"}, nil
}
func (f *fakePeerBackend) AuthenticateAKA(context.Context, *ts43.AKARequest) (*ts43.AKAResponse, error) {
	f.authentication.Add(1)
	return &ts43.AKAResponse{Res: []byte{1}}, nil
}

type peerTestSession struct {
	wire    *peerWire
	broker  *peerBroker
	backend *fakePeerBackend
	changed chan struct{}
	done    chan error
	nextID  uint64
}

func newPeerTestSession(t *testing.T, timeout ...time.Duration) *peerTestSession {
	t.Helper()
	p := &peerTestSession{broker: newPeerBroker(nil), backend: &fakePeerBackend{}, changed: make(chan struct{}, 1), done: make(chan error, 1)}
	if len(timeout) > 0 {
		p.broker.waitTimeout = timeout[0]
	}
	p.backend.authorized.Store(true)
	p.broker.allowed = p.backend.authorized.Load
	p.broker.subscribers[p.changed] = struct{}{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			p.done <- err
			return
		}
		ctx, release, err := p.broker.begin(r.Context())
		if err != nil {
			_ = conn.Close()
			p.done <- err
			return
		}
		defer release()
		wire := newPeerWire(ctx, conn, p.broker.permits)
		defer wire.close()
		err = servePeer(wire, p.broker, p.backend, "127.0.0.1")
		if err != nil {
			_ = wire.send(peerFailure(err))
		}
		p.done <- err
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { _ = p.broker.close(context.Background()) })
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	p.wire = newPeerWire(t.Context(), conn, nil)
	t.Cleanup(p.wire.close)
	return p
}
func (p *peerTestSession) send(t *testing.T, m peerMessage) {
	t.Helper()
	switch m.Type {
	case "aka", "identity", "delete", "close_source", "progress", "finish":
		if m.ID == 0 {
			p.nextID++
			m.ID = p.nextID
		}
	}
	if err := p.wire.send(m); err != nil {
		t.Fatal(err)
	}
}
func (p *peerTestSession) read(t *testing.T, kind string) peerMessage {
	t.Helper()
	m, err := receivePeerWithin(p.wire, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != kind {
		t.Fatalf("type=%q,want %q", m.Type, kind)
	}
	return m
}
func (p *peerTestSession) phase(t *testing.T, phase string) peerRequest {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		for _, r := range p.broker.snapshot() {
			if r.Phase == phase {
				return r
			}
		}
		select {
		case <-p.changed:
		case <-timer.C:
			t.Fatalf("missing phase %s", phase)
		}
	}
}
func (p *peerTestSession) approve(t *testing.T, phase string, accept bool) peerRequest {
	t.Helper()
	r := p.phase(t, phase)
	decision := peerDecision{Phase: phase, Revision: r.Revision, Accept: &accept}
	if accept {
		decision.Selection = &peerSelection{SourceType: SourceModem, SourceID: "modem", ProfileID: "se:8901"}
	}
	if err := p.broker.decide(r.ID, decision); err != nil {
		t.Fatal(err)
	}
	return r
}
func (p *peerTestSession) hello(t *testing.T) {
	p.send(t, peerMessage{Type: "authorize", Version: peerVersion, Target: &peerTarget{Name: "Target", IMEI: strings.Repeat("2", 15), EID: strings.Repeat("3", 32)}})
	reply := p.read(t, "pending")
	if reply.Version != peerVersion || reply.Profile != nil || reply.Device != nil || len(reply.Requests) != 0 || reply.SourceName != "" || reply.SourceEID != "" {
		t.Fatal("handshake disclosed source data")
	}
}

func TestPeerSourceSelectionAndSingleUse(t *testing.T) {
	p := newPeerTestSession(t)
	p.hello(t)
	first := p.phase(t, peerPhaseAuthorization)
	if len(first.Sources) != 1 || len(first.Sources[0].Profiles) != 1 || first.Target.EID != strings.Repeat("3", 32) {
		t.Fatal("source owner did not receive selectable profiles and bound target")
	}
	if p.backend.preparations.Load() != 0 || p.backend.authentication.Load() != 0 {
		t.Fatal("source mutated before approval")
	}
	if _, err := receivePeerWithin(p.wire, 20*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("requester received data before source approval: %v", err)
	}
	p.approve(t, peerPhaseAuthorization, true)
	approved := p.read(t, "approved")
	if approved.Profile == nil || approved.Profile.ICCID != "8901" || approved.SourceName != "Source" || approved.SourceEID != strings.Repeat("1", 32) {
		t.Fatal("approval did not identify the source owner's chosen profile")
	}
	accept := true
	if err := p.broker.decide(first.ID, peerDecision{Phase: first.Phase, Revision: first.Revision, Accept: &accept}); !errors.Is(err, errPeerDecision) {
		t.Fatal("old approval accepted")
	}
	if p.backend.preparations.Load() != 0 {
		t.Fatal("source prepared before target revalidation")
	}
	p.send(t, peerMessage{Type: "prepare"})
	p.read(t, "ready")
	p.send(t, peerMessage{Type: "aka", AKA: &peerAKARequest{Rand: make([]byte, 16), Autn: make([]byte, 16)}})
	p.read(t, "reply")
	p.send(t, peerMessage{Type: "delete", ICCID: "8901"})
	p.read(t, "reply")
	if p.backend.deletions.Load() != 1 || p.backend.releases.Load() != 1 {
		t.Fatal("source deletion did not release its authentication channel")
	}
	p.send(t, peerMessage{Type: "finish", Outcome: "completed"})
	p.read(t, "reply")
	p.wire.close()
	if err := <-p.done; err != nil {
		t.Fatal(err)
	}
	if r := p.phase(t, "ended"); r.Outcome != "completed" || len(r.Sources) != 0 {
		t.Fatal("terminal authorization retained source choices")
	}
	if err := p.broker.decide(first.ID, peerDecision{Phase: first.Phase, Revision: first.Revision, Accept: &accept}); !errors.Is(err, errPeerDecision) {
		t.Fatal("terminal authorization reused")
	}
	if _, err := receivePeerWithin(p.wire, time.Second); err == nil {
		t.Fatal("finished connection remained open")
	}
}

func TestPeerRejectCancelAndInvalidOperations(t *testing.T) {
	for _, scenario := range []string{"reject", "cancel", "requester_selection", "changed_card", "wrong_delete", "duplicate_delete", "early_authentication"} {
		t.Run(scenario, func(t *testing.T) {
			p := newPeerTestSession(t)
			p.hello(t)
			p.phase(t, peerPhaseAuthorization)
			switch scenario {
			case "reject":
				p.approve(t, peerPhaseAuthorization, false)
			case "cancel":
				p.wire.close()
			default:
				if scenario == "requester_selection" {
					p.send(t, peerMessage{Type: "transfer"})
				}
				if scenario == "early_authentication" {
					p.send(t, peerMessage{Type: "aka", AKA: &peerAKARequest{}})
				}
				p.approve(t, peerPhaseAuthorization, true)
				if scenario != "requester_selection" && scenario != "early_authentication" {
					p.read(t, "approved")
					p.backend.changed.Store(scenario == "changed_card")
					p.send(t, peerMessage{Type: "prepare"})
					if scenario != "changed_card" {
						p.read(t, "ready")
						iccid := "wrong"
						if scenario == "duplicate_delete" {
							iccid = "8901"
							p.send(t, peerMessage{Type: "delete", ICCID: iccid})
							p.read(t, "reply")
						}
						p.send(t, peerMessage{Type: "delete", ICCID: iccid})
					}
				}
			}
			select {
			case err := <-p.done:
				if err == nil {
					t.Fatal("invalid flow succeeded")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("session did not terminate")
			}
			p.phase(t, "ended")
			if scenario == "duplicate_delete" {
				if p.backend.deletions.Load() != 1 {
					t.Fatal("delete replayed")
				}
			} else if p.backend.deletions.Load() != 0 {
				t.Fatal("unapproved deletion")
			}
			if p.backend.authentication.Load() != 0 {
				t.Fatal("unapproved authentication")
			}
			if (scenario == "requester_selection" || scenario == "early_authentication") && p.backend.preparations.Load() != 0 {
				t.Fatal("requester changed the approved source")
			}
		})
	}
}

func TestPeerConcurrentSourceApprovals(t *testing.T) {
	p := newPeerTestSession(t)
	p.hello(t)
	r := p.phase(t, peerPhaseAuthorization)
	accept := true
	invalid := peerDecision{Phase: r.Phase, Revision: r.Revision, Accept: &accept,
		Selection: &peerSelection{SourceType: SourceModem, SourceID: "other", ProfileID: "se:8901"}}
	if err := p.broker.decide(r.ID, invalid); !errors.Is(err, ErrProfileUnsupported) {
		t.Fatal("unknown source accepted")
	}
	if err := p.broker.decide(r.ID, peerDecision{Phase: r.Phase, Revision: r.Revision, Accept: &accept}); !errors.Is(err, ErrProfileUnsupported) {
		t.Fatal("missing selection accepted")
	}
	decision := invalid
	decision.Selection = &peerSelection{SourceType: SourceModem, SourceID: "modem", ProfileID: "se:8901"}
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for range 8 {
		wg.Go(func() {
			if p.broker.decide(r.ID, decision) == nil {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d concurrent approvals", accepted.Load())
	}
	p.read(t, "approved")
	p.wire.close()
	<-p.done
}

func TestPeerBrokerDecisionExpiryAndReservation(t *testing.T) {
	b := newPeerBroker(nil)
	p, err := b.create("local", peerTarget{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	beginPeerTestApproval(t, t.Context(), b, p, nil)
	b.mu.Lock()
	p.request.ExpiresAt = time.Now().Add(-time.Second)
	r := p.request
	b.mu.Unlock()
	accept := true
	if err := b.decide(r.ID, peerDecision{Phase: r.Phase, Revision: r.Revision, Accept: &accept}); !errors.Is(err, errPeerDecision) {
		t.Fatal("expired decision accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := b.awaitApproval(ctx, p, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release, err := b.reserve("device")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.reserve("device"); err == nil {
		t.Fatal("concurrent source reservation accepted")
	}
	release()
	release, err = b.reserve("device")
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestPeerAddressPolicy(t *testing.T) {
	u, err := peerURL("https://example.com/prefix/")
	if err != nil || u.String() != "wss://example.com/prefix/api/v1/esim-transfers/remote-sessions" {
		t.Fatalf("%v,%v", u, err)
	}
	for _, raw := range []string{"https://user:password@example.com", "file:///tmp/card", "https://example.com?token=secret", "https://example.com/#x"} {
		if _, err := peerURL(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for raw, want := range map[string]bool{"192.168.1.1": true, "127.0.0.1": true, "100.64.1.2": true, "fd00::1": true, "8.8.8.8": false, "169.254.169.254": false, "0.0.0.0": false} {
		if got := privatePeerAddress(netip.MustParseAddr(raw)); got != want {
			t.Errorf("%s=%v", raw, got)
		}
	}
}

func TestPeerSourceSelectionTimeout(t *testing.T) {
	p := newPeerTestSession(t, 100*time.Millisecond)
	p.hello(t)
	request := p.phase(t, peerPhaseAuthorization)
	select {
	case err := <-p.done:
		if err == nil {
			t.Fatal("expired flow succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expired flow remained open")
	}
	p.phase(t, "ended")
	accept := true
	if err := p.broker.decide(request.ID, peerDecision{Phase: request.Phase, Revision: request.Revision, Accept: &accept}); !errors.Is(err, errPeerDecision) {
		t.Fatal("expired request remained reusable")
	}
	if p.backend.preparations.Load() != 0 {
		t.Fatal("expired authorization modified the source")
	}
}

func TestPeerRejectsUnsupportedVersionsBeforeListingSources(t *testing.T) {
	for _, version := range []int{0, 2} {
		t.Run(fmt.Sprintf("version_%d", version), func(t *testing.T) {
			p := newPeerTestSession(t)
			p.send(t, peerMessage{Type: "authorize", Version: version, Target: &peerTarget{}})
			if err := <-p.done; !errors.Is(err, errPeerUnsupported) {
				t.Fatalf("unsupported protocol: %v", err)
			}
			if p.backend.catalogReads.Load() != 0 {
				t.Fatal("unsupported requester accessed source profiles")
			}
		})
	}
}

func TestPeerRevocationBeforeSourceMutation(t *testing.T) {
	p := newPeerTestSession(t)
	p.hello(t)
	p.approve(t, "authorization", true)
	p.read(t, "approved")
	p.backend.authorized.Store(false)
	p.send(t, peerMessage{Type: "prepare"})
	select {
	case err := <-p.done:
		if err == nil {
			t.Fatal("revoked authorization succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("revoked session remained open")
	}
	if p.backend.preparations.Load() != 0 {
		t.Fatal("source modified after authorization was revoked")
	}
}

func TestPeerReaderChoiceRequiresValidSourceIMEI(t *testing.T) {
	broker := newPeerBroker(nil)
	pending, err := broker.create("local", peerTarget{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog := fakePeerCatalog()
	catalog[0].Source = SourceResponse{Type: SourceCCID, ID: "reader", Name: "Reader", RequiresSourceIMEI: true}
	result := beginPeerTestApproval(t, t.Context(), broker, pending, catalog)
	request := broker.snapshot()[0]
	accept := true
	selection := peerSelection{SourceType: SourceCCID, SourceID: "reader", ProfileID: "se:8901"}
	for _, imei := range []string{"", "12345", strings.Repeat("x", 15)} {
		selection.SourceIMEI = imei
		if err := broker.decide(request.ID, peerDecision{Phase: request.Phase, Revision: request.Revision, Accept: &accept, Selection: &selection}); !errors.Is(err, ErrSourceIMEIRequired) {
			t.Fatalf("IMEI %q: %v", imei, err)
		}
	}
	selection.SourceIMEI = strings.Repeat("2", 15)
	if err := broker.decide(request.ID, peerDecision{Phase: request.Phase, Revision: request.Revision, Accept: &accept, Selection: &selection}); err != nil {
		t.Fatal(err)
	}
	approved := <-result
	if approved.err != nil || approved.approval.selection.SourceID != "reader" || approved.approval.profile.ICCID != "8901" {
		t.Fatalf("incorrect reader approval: %+v", approved)
	}
}

func TestPeerDecisionCannotOutliveItsConnection(t *testing.T) {
	broker := newPeerBroker(nil)
	done := make(chan struct{})
	pending, err := broker.create("local", peerTarget{}, done)
	if err != nil {
		t.Fatal(err)
	}
	result := beginPeerTestApproval(t, t.Context(), broker, pending, fakePeerCatalog())
	request := broker.snapshot()[0]
	close(done)
	accept := true
	selection := peerSelection{SourceType: SourceModem, SourceID: "modem", ProfileID: "se:8901"}
	if err := broker.decide(request.ID, peerDecision{Phase: request.Phase, Revision: request.Revision, Accept: &accept, Selection: &selection}); !errors.Is(err, errPeerDecision) {
		t.Fatalf("disconnected grant accepted: %v", err)
	}
	if approval := <-result; !errors.Is(approval.err, errPeerDecision) {
		t.Fatalf("disconnected approval wait: %v", approval.err)
	}
}

type peerApprovalResult struct {
	approval peerApproval
	err      error
}

func TestPeerApprovalCannotRenewItsDeadline(t *testing.T) {
	broker := newPeerBroker(nil)
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	pending, err := broker.create("local", peerTarget{}, ctx.Done())
	if err != nil {
		t.Fatal(err)
	}
	result := beginPeerTestApproval(t, ctx, broker, pending, fakePeerCatalog())
	request := broker.snapshot()[0]
	if !request.ExpiresAt.Equal(deadline) {
		t.Fatalf("deadline = %v, want %v", request.ExpiresAt, deadline)
	}
	if _, err := broker.awaitApproval(t.Context(), pending, fakePeerCatalog()); !errors.Is(err, errPeerDecision) {
		t.Fatalf("reopened approval: %v", err)
	}
	latest := broker.snapshot()[0]
	if latest.Revision != request.Revision || !latest.ExpiresAt.Equal(deadline) {
		t.Fatal("reopening renewed the original approval")
	}
	accept := true
	selection := peerSelection{SourceType: SourceModem, SourceID: "modem", ProfileID: "se:8901"}
	if err := broker.decide(request.ID, peerDecision{Phase: request.Phase, Revision: request.Revision, Accept: &accept, Selection: &selection}); err != nil {
		t.Fatal(err)
	}
	if approved := <-result; approved.err != nil || approved.approval.profile.ICCID != "8901" {
		t.Fatalf("original approval: %+v", approved)
	}
}

func beginPeerTestApproval(t *testing.T, ctx context.Context, broker *peerBroker, pending *peerPending, catalog []peerCatalogSource) <-chan peerApprovalResult {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	t.Cleanup(func() { cancel(); <-stopped })
	changed, unsubscribe := broker.subscribe()
	defer unsubscribe()
	result := make(chan peerApprovalResult, 1)
	go func() {
		defer close(stopped)
		approval, err := broker.awaitApproval(ctx, pending, catalog)
		result <- peerApprovalResult{approval: approval, err: err}
	}()
	for {
		for _, request := range broker.snapshot() {
			if request.ID == pending.request.ID && request.Phase == peerPhaseAuthorization {
				return result
			}
		}
		select {
		case <-changed:
		case <-time.After(2 * time.Second):
			t.Fatal("approval was not published")
		}
	}
}
