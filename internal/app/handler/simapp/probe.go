package simapp

import (
	"context"
	"errors"
	"sync"
	"time"

	stkpkg "github.com/damonto/uicc-go/usim/stk"
)

const (
	setupMenuProbeDelay   = 1500 * time.Millisecond
	probeSelectionTimeout = 5 * time.Second
)

var setupMenuProbeInterval = 100 * time.Millisecond

type rootMenuSelector interface {
	SelectMenu(context.Context, byte, bool) (stkpkg.EnvelopeResponse, error)
}

type envelopeRootSelector struct {
	sender envelopeSender
}

type envelopeSender interface {
	SendEnvelope(context.Context, stkpkg.Envelope) (stkpkg.EnvelopeResponse, error)
}

func (s envelopeRootSelector) SelectMenu(ctx context.Context, item byte, helpRequested bool) (stkpkg.EnvelopeResponse, error) {
	return s.sender.SendEnvelope(ctx, stkpkg.MenuSelection(item, helpRequested))
}

type probeState struct {
	mu           sync.RWMutex
	scan         probeScanState
	scanSeq      uint64
	selection    probeRootSelection
	selectionSeq uint64
}

type probeHitKind uint8

const (
	probeHitSelectItem probeHitKind = iota + 1
	probeHitDisplayText
)

type probeScanState struct {
	active bool
	seq    uint64
	item   byte
	hitCh  chan probeScanHit
}

type probeScanHit struct {
	kind probeHitKind
	item byte
}

type probeRootSelection struct {
	active        bool
	seq           uint64
	item          byte
	helpRequested bool
	probeItem     byte
	hitCh         chan probeHitKind
}

func (p *probeState) clear() {
	p.mu.Lock()
	p.scan = probeScanState{}
	p.selection = probeRootSelection{}
	p.mu.Unlock()
}

func (p *probeState) beginScanItem(item byte) (uint64, <-chan probeScanHit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scanSeq++
	seq := p.scanSeq
	hitCh := make(chan probeScanHit, 1)
	p.scan = probeScanState{
		active: true,
		seq:    seq,
		item:   item,
		hitCh:  hitCh,
	}
	return seq, hitCh
}

func (p *probeState) clearScanItem(seq uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.scan.active && p.scan.seq == seq {
		p.scan = probeScanState{}
	}
}

func (p *probeState) takeScanHit(kind probeHitKind) (probeScanHit, bool) {
	p.mu.Lock()
	if !p.scan.active {
		p.mu.Unlock()
		return probeScanHit{}, false
	}
	scan := p.scan
	if kind == probeHitSelectItem {
		p.scan = probeScanState{}
	}
	p.mu.Unlock()

	hit := probeScanHit{
		kind: kind,
		item: scan.item,
	}
	sendLatestProbeHit(scan.hitCh, hit)
	return hit, true
}

func (p *probeState) scanning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.scan.active
}

func sendLatestProbeHit(ch chan probeScanHit, hit probeScanHit) {
	select {
	case ch <- hit:
	default:
		select {
		case <-ch:
		default:
		}
		ch <- hit
	}
}

func (p *probeState) expectSelection(item byte, helpRequested bool, probeItem byte) (uint64, <-chan probeHitKind) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selectionSeq++
	seq := p.selectionSeq
	hitCh := make(chan probeHitKind, 1)
	p.selection = probeRootSelection{
		active:        true,
		seq:           seq,
		item:          item,
		helpRequested: helpRequested,
		probeItem:     probeItem,
		hitCh:         hitCh,
	}
	return seq, hitCh
}

func (p *probeState) clearSelection(seq uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if seq != 0 && (!p.selection.active || p.selection.seq != seq) {
		return false
	}
	if !p.selection.active {
		return false
	}
	p.selection = probeRootSelection{}
	return true
}

func (p *probeState) takeSelection() (probeRootSelection, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.selection.active {
		return probeRootSelection{}, false
	}
	selection := p.selection
	p.selection = probeRootSelection{}
	return selection, true
}

func (p *probeState) awaitingSelection() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.selection.active
}

func (s *wsSession) probeMissingSetupMenu(ctx context.Context, sender envelopeSender) (bool, error) {
	timer := time.NewTimer(setupMenuProbeDelay)
	defer timer.Stop()
	select {
	case <-s.setupMenuSeen():
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
	}
	return s.sendSetupMenuProbes(ctx, sender)
}

func (s *wsSession) sendSetupMenuProbes(ctx context.Context, sender envelopeSender) (bool, error) {
	for _, item := range probeItems(0, false) {
		select {
		case <-s.setupMenuSeen():
			return false, nil
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}
		seq, hitCh := s.probe.beginScanItem(item)
		resp, err := sender.SendEnvelope(ctx, stkpkg.MenuSelection(item, false))
		if err != nil {
			s.probe.clearScanItem(seq)
			return false, err
		}
		if !envelopeAccepted(resp) {
			s.probe.clearScanItem(seq)
			continue
		}
		for {
			hit, ok, err := s.waitProbeScanHit(ctx, seq, hitCh)
			if err != nil {
				return false, err
			}
			if !ok {
				break
			}
			if hit.kind == probeHitSelectItem {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *wsSession) waitProbeScanHit(ctx context.Context, seq uint64, hitCh <-chan probeScanHit) (probeScanHit, bool, error) {
	if setupMenuProbeInterval <= 0 {
		select {
		case hit := <-hitCh:
			return hit, true, nil
		default:
			s.probe.clearScanItem(seq)
			return probeScanHit{}, false, nil
		}
	}

	timer := time.NewTimer(setupMenuProbeInterval)
	defer timer.Stop()
	select {
	case hit := <-hitCh:
		return hit, true, nil
	case <-s.setupMenuSeen():
		s.probe.clearScanItem(seq)
		return probeScanHit{}, false, nil
	case <-ctx.Done():
		s.probe.clearScanItem(seq)
		return probeScanHit{}, false, ctx.Err()
	case <-timer.C:
		s.probe.clearScanItem(seq)
		return probeScanHit{}, false, nil
	}
}

func (s *wsSession) rootSelectionLoop(ctx context.Context, selector rootMenuSelector) {
	for {
		select {
		case msg := <-s.rootCh:
			if err := s.selectRootMenu(ctx, selector, msg); err != nil {
				s.sendIfConnected(wsServerMessage{Type: wsTypeError, Message: err.Error()})
			}
		case <-ctx.Done():
			return
		case <-s.disconnectCh:
			return
		}
	}
}

func (s *wsSession) selectRootMenu(ctx context.Context, selector rootMenuSelector, msg wsClientMessage) error {
	item, ok := byteFromItemID(msg.ItemID)
	if !ok {
		return errors.New("menu item id is out of range")
	}
	source, probeItem, hasProbeItem := s.rootMenuAction()
	if source != menuSourceProbe {
		_, err := selector.SelectMenu(ctx, item, msg.HelpRequested)
		return err
	}
	return s.activateProbeRootMenu(ctx, selector, item, msg.HelpRequested, probeItem, hasProbeItem)
}

func (s *wsSession) activateProbeRootMenu(ctx context.Context, selector rootMenuSelector, item byte, helpRequested bool, preferredProbeItem byte, hasPreferredProbeItem bool) error {
	for i, probeItem := range probeItems(preferredProbeItem, hasPreferredProbeItem) {
		if i > 0 {
			if err := sleepProbeInterval(ctx); err != nil {
				return err
			}
		}
		seq, hitCh := s.probe.expectSelection(item, helpRequested, probeItem)
		resp, err := selector.SelectMenu(ctx, probeItem, false)
		if err != nil {
			s.probe.clearSelection(seq)
			return err
		}
		if !envelopeAccepted(resp) {
			s.probe.clearSelection(seq)
			continue
		}
		ok, err := s.waitProbeSelectionHit(ctx, seq, hitCh, probeSelectionWait(i, hasPreferredProbeItem))
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return errors.New("probe SIM Application menu: no activation item accepted")
}

func (s *wsSession) waitProbeSelectionHit(ctx context.Context, seq uint64, hitCh <-chan probeHitKind, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		select {
		case kind := <-hitCh:
			if kind == probeHitSelectItem {
				return true, nil
			}
			s.probe.clearSelection(seq)
			return false, nil
		default:
			s.probe.clearSelection(seq)
			return false, nil
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case kind := <-hitCh:
		if kind == probeHitSelectItem {
			return true, nil
		}
		s.probe.clearSelection(seq)
		return false, nil
	case <-s.setupMenuSeen():
		s.probe.clearSelection(seq)
		return false, nil
	case <-ctx.Done():
		s.probe.clearSelection(seq)
		return false, ctx.Err()
	case <-s.disconnectCh:
		s.probe.clearSelection(seq)
		return false, nil
	case <-timer.C:
		s.probe.clearSelection(seq)
		return false, nil
	}
}

func probeSelectionWait(index int, hasPreferred bool) time.Duration {
	if setupMenuProbeInterval <= 0 {
		return 0
	}
	if hasPreferred && index == 0 {
		return probeSelectionTimeout
	}
	return setupMenuProbeInterval
}

func envelopeAccepted(resp stkpkg.EnvelopeResponse) bool {
	return resp.OK() || resp.HasMore()
}

func sendProbeSelectionHit(ch chan probeHitKind, kind probeHitKind) {
	if ch == nil {
		return
	}
	select {
	case ch <- kind:
	default:
	}
}

func probeItems(preferred byte, hasPreferred bool) []byte {
	items := make([]byte, 0, 256)
	if hasPreferred {
		items = append(items, preferred)
	}
	for i := range 256 {
		item := byte(i)
		if !hasPreferred || item != preferred {
			items = append(items, item)
		}
	}
	return items
}

func sleepProbeInterval(ctx context.Context) error {
	if setupMenuProbeInterval <= 0 {
		return nil
	}
	timer := time.NewTimer(setupMenuProbeInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
