package esimtransfer

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

const (
	peerWaitTimeout   = 5 * time.Minute
	peerMaxRequests   = 64
	peerMaxPerAddress = 5
)

var (
	errPeerDecision = errors.New("transfer request is no longer awaiting this decision")
	errPeerDeclined = errors.New("remote transfer request was declined")
	errPeerExpired  = errors.New("remote transfer approval expired")
	errPeerClosed   = errors.New("remote transfer service is closed")
	errPeerCapacity = errors.New("too many remote transfer requests")
)

const (
	peerPhaseAuthorization = "authorization"
	peerPhaseDecided       = "decided"
	peerPhaseRunning       = "running"
	peerPhaseEnded         = "ended"
	peerOutcomeFailed      = "failed"
	peerOutcomeCompleted   = "completed"
	peerOutcomeUncertain   = "uncertain"
)

type peerTarget struct {
	Name string `json:"name"`
	IMEI string `json:"imei"`
	EID  string `json:"eid"`
}

type peerRequest struct {
	ID            string             `json:"id"`
	Revision      int                `json:"revision"`
	Phase         string             `json:"phase"`
	RemoteAddress string             `json:"remoteAddress"`
	Target        peerTarget         `json:"target"`
	Sources       []peerSourceOption `json:"sources,omitempty"`
	SourceName    string             `json:"sourceName,omitempty"`
	Profile       *ProfileResponse   `json:"profile,omitempty"`
	Stage         string             `json:"stage,omitempty"`
	Outcome       string             `json:"outcome,omitempty"`
	ExpiresAt     time.Time          `json:"expiresAt"`
}

type peerDecision struct {
	Phase     string         `json:"phase"`
	Revision  int            `json:"revision"`
	Accept    *bool          `json:"accept"`
	Selection *peerSelection `json:"selection,omitempty"`
}

type peerSourceOption struct {
	Source   SourceResponse    `json:"source"`
	Profiles []ProfileResponse `json:"profiles"`
}

type peerApproval struct {
	selection  peerSelection
	profile    ProfileResponse
	sourceName string
	eid        string
}

type peerPending struct {
	done    <-chan struct{}
	request peerRequest
	answer  chan *peerApproval
	catalog []peerCatalogSource
}

// peerBroker owns only in-memory approvals and device reservations. A decision
// never creates a bearer credential: its owner is the original peer connection.
type peerBroker struct {
	mu          sync.Mutex
	requests    map[string]*peerPending
	subscribers map[chan struct{}]struct{}
	busy        map[string]bool
	allowed     func() bool
	waitTimeout time.Duration
	closed      atomic.Bool
	scopes      map[*peerScope]struct{}
	workers     sync.WaitGroup
	shutdown    sync.Once
	stopped     chan struct{}
}

func newPeerBroker(allowed func() bool) *peerBroker {
	return &peerBroker{
		requests:    make(map[string]*peerPending),
		subscribers: make(map[chan struct{}]struct{}),
		busy:        make(map[string]bool),
		scopes:      make(map[*peerScope]struct{}),
		stopped:     make(chan struct{}),
		allowed:     allowed,
		waitTimeout: peerWaitTimeout,
	}
}

func (b *peerBroker) permits() bool { return !b.closed.Load() && (b.allowed == nil || b.allowed()) }

func (b *peerBroker) publishLocked() {
	for ch := range b.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (b *peerBroker) create(remote string, target peerTarget, done <-chan struct{}) (*peerPending, error) {
	if !b.permits() {
		return nil, errPeerClosed
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, p := range b.requests {
		if p.request.Phase == peerPhaseEnded && time.Now().After(p.request.ExpiresAt) {
			delete(b.requests, id)
		}
	}
	sameAddress := 0
	for _, existing := range b.requests {
		if existing.request.RemoteAddress == remote && existing.request.Phase != peerPhaseEnded {
			sameAddress++
		}
	}
	if len(b.requests) >= peerMaxRequests || sameAddress >= peerMaxPerAddress || b.closed.Load() {
		return nil, errors.New("remote transfer requests are unavailable")
	}
	p := &peerPending{done: done, request: peerRequest{ID: rand.Text(), Target: target, RemoteAddress: remote}, answer: make(chan *peerApproval, 1)}
	b.requests[p.request.ID] = p
	return p, nil
}

func (b *peerBroker) update(p *peerPending, phase, outcome string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p.request.Phase == peerPhaseEnded {
		return
	}
	p.request.Phase, p.request.Outcome = phase, outcome
	p.request.Revision++
	p.request.ExpiresAt = time.Now().Add(b.waitTimeout)
	if phase == peerPhaseEnded {
		p.request.ExpiresAt = time.Now().Add(time.Minute)
		p.request.Sources, p.catalog = nil, nil
	}
	b.publishLocked()
}

func (b *peerBroker) awaitApproval(ctx context.Context, p *peerPending, catalog []peerCatalogSource) (peerApproval, error) {
	if err := ctx.Err(); err != nil {
		return peerApproval{}, err
	}
	expires := time.Now().Add(b.waitTimeout)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(expires) {
		expires = deadline
	}
	b.mu.Lock()
	if err := ctx.Err(); err != nil {
		b.mu.Unlock()
		return peerApproval{}, err
	}
	if p.request.Phase != "" || !b.permits() {
		b.mu.Unlock()
		return peerApproval{}, errPeerDecision
	}
	select {
	case <-p.done:
		b.mu.Unlock()
		return peerApproval{}, errPeerDecision
	default:
	}
	// Publish the choices and their actual deadline in one transition. The
	// enumeration time is part of the same authorization timeout.
	p.catalog = catalog
	for _, entry := range catalog {
		p.request.Sources = append(p.request.Sources, peerSourceOption{Source: entry.Source, Profiles: entry.Profiles})
	}
	p.request.Phase = peerPhaseAuthorization
	p.request.Revision++
	p.request.ExpiresAt = expires
	b.publishLocked()
	b.mu.Unlock()
	timer := time.NewTimer(time.Until(expires))
	defer timer.Stop()
	select {
	case approval := <-p.answer:
		if approval == nil {
			return peerApproval{}, errPeerDeclined
		}
		if err := ctx.Err(); err != nil {
			return peerApproval{}, err
		}
		if !b.permits() {
			return peerApproval{}, errPeerDecision
		}
		select {
		case <-p.done:
			return peerApproval{}, errPeerDecision
		default:
		}
		return *approval, nil
	case <-ctx.Done():
		return peerApproval{}, ctx.Err()
	case <-p.done:
		return peerApproval{}, errPeerDecision
	case <-timer.C:
		return peerApproval{}, errPeerExpired
	}
}

func (b *peerBroker) decide(id string, decision peerDecision) error {
	if !b.permits() {
		return errPeerDecision
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.requests[id]
	if p == nil || b.closed.Load() || decision.Accept == nil ||
		p.request.Revision != decision.Revision || p.request.Phase != decision.Phase ||
		decision.Phase != peerPhaseAuthorization || time.Now().After(p.request.ExpiresAt) {
		return errPeerDecision
	}
	select {
	case <-p.done:
		return errPeerDecision
	default:
	}
	var approval *peerApproval
	if *decision.Accept {
		if decision.Selection == nil {
			return ErrProfileUnsupported
		}
		selection := *decision.Selection
		profile, eid, name, ok := catalogSelection(p.catalog, selection)
		if !ok {
			return ErrProfileUnsupported
		}
		if selection.SourceType == SourceCCID && !validPeerIMEI(selection.SourceIMEI) {
			return ErrSourceIMEIRequired
		}
		approval = &peerApproval{selection: selection, profile: profile, sourceName: name, eid: eid}
		p.request.SourceName, p.request.Profile = name, &profile
	}
	// Change phase under the same lock so a second UI cannot approve it again.
	p.request.Phase = peerPhaseDecided
	p.request.Revision++
	p.request.Sources, p.catalog = nil, nil
	p.answer <- approval
	b.publishLocked()
	return nil
}

func (b *peerBroker) snapshot() []peerRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]peerRequest, 0, len(b.requests))
	for _, p := range b.requests {
		if p.request.Phase != "" && (p.request.Phase != peerPhaseEnded || time.Now().Before(p.request.ExpiresAt)) {
			result = append(result, p.request)
		}
	}
	slices.SortFunc(result, func(a, c peerRequest) int { return a.ExpiresAt.Compare(c.ExpiresAt) })
	return result
}

func (b *peerBroker) reserve(key string) (func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.busy[key] {
		return nil, errors.New("transfer source is busy")
	}
	b.busy[key] = true
	return func() { b.mu.Lock(); delete(b.busy, key); b.mu.Unlock() }, nil
}

type peerScope struct {
	cancel context.CancelFunc
	remote string
}

// begin owns each hijacked request through hardware cleanup, not just until its
// socket closes. Shutdown cancels these contexts before the LPA pools are closed.
func (b *peerBroker) begin(ctx context.Context) (context.Context, func(), error) {
	return b.beginScope(ctx, "")
}

func (b *peerBroker) beginRemote(ctx context.Context, remote string) (context.Context, func(), error) {
	return b.beginScope(ctx, remote)
}

func (b *peerBroker) beginScope(ctx context.Context, remote string) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed.Load() {
		return nil, nil, errPeerClosed
	}
	if remote != "" {
		total, sameAddress := 0, 0
		for scope := range b.scopes {
			if scope.remote != "" {
				total++
			}
			if scope.remote == remote {
				sameAddress++
			}
		}
		if total >= peerMaxRequests || sameAddress >= peerMaxPerAddress {
			return nil, nil, errPeerCapacity
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	scope := &peerScope{cancel: cancel, remote: remote}
	b.scopes[scope] = struct{}{}
	b.workers.Add(1)
	return ctx, func() {
		cancel()
		b.mu.Lock()
		delete(b.scopes, scope)
		b.mu.Unlock()
		b.workers.Done()
	}, nil
}

func (b *peerBroker) close(ctx context.Context) error {
	b.shutdown.Do(func() {
		b.mu.Lock()
		b.closed.Store(true)
		for scope := range b.scopes {
			scope.cancel()
		}
		b.mu.Unlock()
		// No new scopes can be added after closed is set. The stopped channel also
		// lets a later cleanup caller join a shutdown whose earlier deadline expired.
		go func() { b.workers.Wait(); close(b.stopped) }()
	})
	select {
	case <-b.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *peerBroker) subscribe() (<-chan struct{}, func()) {
	changed := make(chan struct{}, 1)
	b.mu.Lock()
	b.subscribers[changed] = struct{}{}
	b.mu.Unlock()
	return changed, func() { b.mu.Lock(); delete(b.subscribers, changed); b.mu.Unlock() }
}

func (b *peerBroker) progress(p *peerPending, stage string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p.request.Phase != peerPhaseRunning {
		return
	}
	p.request.Stage = stage
	b.publishLocked()
}
