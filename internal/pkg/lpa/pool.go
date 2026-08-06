package lpa

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

var (
	errPoolClosed       = errors.New("LPA client pool is closed")
	errPoolModemRetired = errors.New("modem generation is retired")
	errPoolRequired     = errors.New("LPA client pool is required")
	errPoolEntryRetired = errors.New("LPA client entry is retired")
)

// Pool owns long-lived, slot-scoped eUICC clients. A client is retained until
// its SIM identity changes, its modem generation retires, or the pool closes.
// A lease serializes APDU operations because an eUICC channel is not safe for
// concurrent use.
type Pool struct {
	store    *settings.Store
	registry *modem.Registry

	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu          sync.Mutex
	closed      bool
	entries     map[poolKey]*poolEntry
	creating    map[poolKey]chan struct{}
	failures    map[poolKey]error
	discovering map[poolSEKey]chan struct{}
	secureElems map[poolSEKey][]SE
	slotEpoch   map[poolSEKey]uint64
	retired     map[*modem.Modem]struct{}
	unsubscribe func()
}

type poolKey struct {
	modem *modem.Modem
	slot  uint8
	seID  string
}

type poolEntry struct {
	pool    *Pool
	key     poolKey
	modem   *modem.Modem
	client  *Client
	gate    chan struct{}
	done    chan struct{}
	err     error
	lockKey string
}

type poolSEKey struct {
	modem *modem.Modem
	slot  uint8
}

type poolTarget struct {
	se       SE
	slot     uint8
	sourceID string
}

// NewPool starts eager LPA creation for each modem's active SIM slot and
// subscribes to future modem generations. Entries are refreshed after a SIM
// change and closed on modem replacement or pool shutdown.
func NewPool(store *settings.Store, registry *modem.Registry) (*Pool, error) {
	if store == nil {
		return nil, errors.New("settings store is required")
	}
	if registry == nil {
		return nil, errors.New("modem registry is required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	pool := &Pool{
		store:       store,
		registry:    registry,
		cancel:      cancel,
		entries:     make(map[poolKey]*poolEntry),
		creating:    make(map[poolKey]chan struct{}),
		failures:    make(map[poolKey]error),
		discovering: make(map[poolSEKey]chan struct{}),
		secureElems: make(map[poolSEKey][]SE),
		slotEpoch:   make(map[poolSEKey]uint64),
		retired:     make(map[*modem.Modem]struct{}),
	}
	unsubscribe, err := registry.Subscribe(func(event modem.ModemEvent) error {
		pool.handleModemEvent(ctx, event)
		return nil
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("subscribe LPA modem lifecycle: %w", err)
	}
	pool.unsubscribe = unsubscribe

	modems, err := registry.Modems(ctx)
	if err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("list modems for LPA clients: %w", err)
	}
	for _, current := range modems {
		pool.warm(ctx, current)
	}
	return pool, nil
}

// SecureElements returns usable eUICC targets from the active SIM slot.
// Discovery and successful clients are retained by the pool, so a read-only
// refresh does not reopen a QMI or MBIM smart-card client.
func (p *Pool) SecureElements(ctx context.Context, m *modem.Modem) ([]SE, error) {
	if p == nil {
		return nil, errPoolRequired
	}
	if m == nil {
		return nil, errors.New("modem is required")
	}
	targets, err := p.targets(ctx, m)
	if err != nil {
		return nil, err
	}
	ses := make([]SE, len(targets))
	for i, target := range targets {
		ses[i] = target.se
	}
	return ses, nil
}

// Acquire returns a serialized lease of the client for the selected eUICC.
// Call Close when the operation completes.
func (p *Pool) Acquire(ctx context.Context, m *modem.Modem, seID string) (*Lease, error) {
	if p == nil {
		return nil, errPoolRequired
	}
	if m == nil {
		return nil, errors.New("modem is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errPoolClosed
	}
	if p.isRetiredLocked(m) {
		p.mu.Unlock()
		return nil, errPoolModemRetired
	}
	p.mu.Unlock()
	seID = strings.TrimSpace(seID)
	if seID == "" {
		return nil, ErrSERequired
	}
	targets, err := p.targets(ctx, m)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		if target.se.ID == seID {
			key := poolKey{modem: m, slot: target.slot, seID: target.sourceID}
			entry := p.entry(key)
			if entry == nil {
				return nil, fmt.Errorf("%w: %s", ErrSENotFound, seID)
			}
			return lease(ctx, entry)
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrSENotFound, seID)
}

func (p *Pool) targets(ctx context.Context, m *modem.Modem) ([]poolTarget, error) {
	var (
		targets []poolTarget
		errs    error
	)
	for _, slot := range poolSlots(m) {
		ses, err := p.secureElementsForSlot(ctx, m, slot)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("discover eUICC in SIM slot %d: %w", slot, err))
			continue
		}
		for _, se := range ses {
			key := poolKey{modem: m, slot: slot, seID: se.ID}
			if p.entry(key) == nil {
				client, err := p.createAndLease(ctx, key, se.AID)
				if err != nil {
					errs = errors.Join(errs, fmt.Errorf("create LPA client for SIM slot %d SE %s: %w", slot, se.ID, err))
					continue
				}
				if err := client.Close(); err != nil {
					errs = errors.Join(errs, fmt.Errorf("release LPA client for SIM slot %d SE %s: %w", slot, se.ID, err))
					continue
				}
			}
			targets = append(targets, poolTarget{se: se, slot: slot, sourceID: se.ID})
		}
	}
	if len(targets) == 0 {
		if errs != nil {
			return nil, errs
		}
		return nil, ErrNoSupportedAID
	}
	return exposeTargets(targets), nil
}

func exposeTargets(targets []poolTarget) []poolTarget {
	counts := make(map[string]int, len(targets))
	for _, target := range targets {
		counts[target.sourceID]++
	}
	seen := make(map[string]bool, len(counts))
	for i := range targets {
		target := &targets[i]
		if counts[target.sourceID] == 1 {
			continue
		}
		target.se.Label = fmt.Sprintf("%s (SIM slot %d)", target.se.Label, target.slot)
		if seen[target.sourceID] {
			target.se.ID = fmt.Sprintf("%s-slot%d", target.sourceID, target.slot)
		}
		seen[target.sourceID] = true
	}
	return targets
}

func (p *Pool) entry(key poolKey) *poolEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	return p.entries[key]
}

func (p *Pool) createAndLease(ctx context.Context, key poolKey, aid []byte) (*Lease, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errPoolClosed
	}
	if p.isRetiredLocked(key.modem) {
		p.mu.Unlock()
		return nil, errPoolModemRetired
	}
	if err := p.failures[key]; err != nil {
		p.mu.Unlock()
		return nil, err
	}
	if entry := p.entries[key]; entry != nil {
		p.mu.Unlock()
		return lease(ctx, entry)
	}
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return nil, err
	}
	if ready := p.creating[key]; ready != nil {
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ready:
			return p.createAndLease(ctx, key, aid)
		}
	}
	ready := make(chan struct{})
	p.creating[key] = ready
	seKey := poolSEKey{modem: key.modem, slot: key.slot}
	epoch := p.slotEpoch[seKey]
	p.mu.Unlock()

	current := p.store.Snapshot()
	releaseSIMSlot, err := key.modem.ReserveSIMSlot(ctx)
	if err != nil {
		p.finishCreation(key, ready)
		return nil, fmt.Errorf("reserve pooled LPA SIM slot: %w", err)
	}
	activeSlot, err := modem.ActiveSIMSlot(key.modem)
	if err != nil {
		releaseSIMSlot()
		p.finishCreation(key, ready)
		return nil, err
	}
	if activeSlot != key.slot {
		releaseSIMSlot()
		p.finishCreation(key, ready)
		return nil, errSIMSlotChanged
	}

	lockKey := lpaLockKey(key.modem, key.slot)
	if err := gmu.LockContext(ctx, lockKey); err != nil {
		releaseSIMSlot()
		p.finishCreation(key, ready)
		return nil, fmt.Errorf("reserve LPA client: %w", err)
	}
	client, err := newClientForSlot(ctx, modemClientConfig{
		modem:    key.modem,
		settings: &current,
		slot:     key.slot,
		aid:      aid,
	})
	p.mu.Lock()
	ownsReady := p.creating[key] == ready
	if ownsReady {
		delete(p.creating, key)
	}
	abortCreation := func(cause error) (*Lease, error) {
		p.mu.Unlock()
		closeErr := closeClientWhileLocked(client)
		gmu.Unlock(lockKey)
		releaseSIMSlot()
		if ownsReady {
			close(ready)
		}
		return nil, errors.Join(cause, closeErr)
	}
	if p.slotEpoch[seKey] != epoch {
		return abortCreation(errors.Join(errSIMSlotChanged, err))
	}
	if err != nil {
		if errors.Is(err, errCacheableNoSupportedAID) {
			if p.failures == nil {
				p.failures = make(map[poolKey]error)
			}
			p.failures[key] = err
		}
		return abortCreation(err)
	}
	if err := ctx.Err(); err != nil {
		return abortCreation(err)
	}
	if p.closed {
		return abortCreation(errPoolClosed)
	}
	if p.isRetiredLocked(key.modem) {
		return abortCreation(errPoolModemRetired)
	}
	if entry := p.entries[key]; entry != nil {
		if _, closeErr := abortCreation(nil); closeErr != nil {
			return nil, closeErr
		}
		return lease(ctx, entry)
	}
	entry := &poolEntry{
		pool:    p,
		key:     key,
		modem:   key.modem,
		client:  client,
		gate:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		lockKey: lockKey,
	}
	p.entries[key] = entry
	p.mu.Unlock()
	if ownsReady {
		close(ready)
	}
	return newPoolLease(ctx, entry, releaseSIMSlot), nil
}

func (p *Pool) finishCreation(key poolKey, ready chan struct{}) {
	p.mu.Lock()
	if p.creating[key] == ready {
		delete(p.creating, key)
		close(ready)
	}
	p.mu.Unlock()
}

func closeClientWhileLocked(client *Client) error {
	if client == nil {
		return nil
	}
	return client.Close()
}

func lease(ctx context.Context, entry *poolEntry) (*Lease, error) {
	if entry == nil {
		return nil, errPoolRequired
	}
	select {
	case <-entry.done:
		return nil, entry.err
	default:
	}
	var releaseSIMSlot func()
	if entry.modem != nil {
		var err error
		releaseSIMSlot, err = entry.modem.ReserveSIMSlot(ctx)
		if err != nil {
			return nil, fmt.Errorf("reserve pooled LPA SIM slot: %w", err)
		}
	}
	select {
	case <-ctx.Done():
		releaseReservation(releaseSIMSlot)
		return nil, ctx.Err()
	case <-entry.done:
		releaseReservation(releaseSIMSlot)
		return nil, entry.err
	case <-entry.gate:
	}
	select {
	case <-entry.done:
		entry.gate <- struct{}{}
		releaseReservation(releaseSIMSlot)
		return nil, entry.err
	default:
	}
	if entry.lockKey != "" {
		if err := gmu.LockContext(ctx, entry.lockKey); err != nil {
			entry.gate <- struct{}{}
			releaseReservation(releaseSIMSlot)
			return nil, err
		}
		select {
		case <-entry.done:
			gmu.Unlock(entry.lockKey)
			entry.gate <- struct{}{}
			releaseReservation(releaseSIMSlot)
			return nil, entry.err
		default:
		}
	}
	return newPoolLease(ctx, entry, releaseSIMSlot), nil
}

func newPoolLease(ctx context.Context, entry *poolEntry, releaseSIMSlot func()) *Lease {
	entry.client.operation.use(ctx)
	return newLease(entry.client, func(disposition leaseDisposition) error {
		defer func() {
			if entry.lockKey != "" {
				gmu.Unlock(entry.lockKey)
			}
			entry.gate <- struct{}{}
			releaseReservation(releaseSIMSlot)
		}()

		entry.client.operation.reset()
		if disposition == leaseReusable {
			return nil
		}
		if entry.pool != nil {
			entry.pool.mu.Lock()
			if entry.pool.entries[entry.key] == entry {
				delete(entry.pool.entries, entry.key)
				closePoolEntryLocked(entry, errPoolEntryRetired)
			}
			entry.pool.mu.Unlock()
		}
		return entry.client.discard()
	})
}

func releaseReservation(release func()) {
	if release != nil {
		release()
	}
}

func (p *Pool) warm(ctx context.Context, m *modem.Modem) {
	if p == nil || m == nil {
		return
	}
	p.mu.Lock()
	if p.closed || p.isRetiredLocked(m) {
		p.mu.Unlock()
		return
	}
	p.wg.Go(func() {
		p.warmModem(ctx, m)
	})
	p.mu.Unlock()
}

func (p *Pool) warmModem(ctx context.Context, m *modem.Modem) {
	if err := ctx.Err(); err != nil {
		return
	}
	if !waitForWarmableEUICC(ctx, m) {
		return
	}
	for _, slot := range poolSlots(m) {
		se, err := p.secureElementsForSlot(ctx, m, slot)
		if err != nil {
			m.Logger().Debug("discover eUICC secure elements for LPA pool", "slot", slot, "error", err)
			continue
		}
		for _, currentSE := range se {
			key := poolKey{modem: m, slot: slot, seID: currentSE.ID}
			client, err := p.createAndLease(ctx, key, currentSE.AID)
			if err != nil {
				if !errors.Is(err, ErrNoSupportedAID) && ctx.Err() == nil {
					m.Logger().Debug("create LPA client", "slot", slot, "seId", currentSE.ID, "error", err)
				}
				continue
			}
			// The prewarm lease has no operation to run.
			_ = client.Close()
		}
	}
}

func waitForWarmableEUICC(ctx context.Context, m *modem.Modem) bool {
	const pollInterval = 100 * time.Millisecond
	for {
		ready, settled := warmableEUICCState(m.Snapshot())
		if settled {
			return ready
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func warmableEUICCState(snapshot modem.ModemSnapshot) (ready, settled bool) {
	switch snapshot.SIMKind() {
	case modem.SIMKindEUICC:
		isReady := snapshot.Status.SIM == wwanmodem.SIMStateReady &&
			snapshot.SIM != nil && strings.TrimSpace(snapshot.SIM.Identifier) != ""
		return isReady, isReady
	case modem.SIMKindPhysical:
		return false, true
	default:
		return false, snapshot.Status.SIM == wwanmodem.SIMStateAbsent
	}
}

func (p *Pool) secureElementsForSlot(ctx context.Context, m *modem.Modem, slot uint8) ([]SE, error) {
	key := poolSEKey{modem: m, slot: slot}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errPoolClosed
	}
	if p.isRetiredLocked(m) {
		p.mu.Unlock()
		return nil, errPoolModemRetired
	}
	if cached, ok := p.secureElems[key]; ok {
		out := cloneSEs(cached)
		p.mu.Unlock()
		return out, nil
	}
	if p.discovering == nil {
		p.discovering = make(map[poolSEKey]chan struct{})
	}
	if ready := p.discovering[key]; ready != nil {
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ready:
			return p.secureElementsForSlot(ctx, m, slot)
		}
	}
	ready := make(chan struct{})
	p.discovering[key] = ready
	epoch := p.slotEpoch[key]
	p.mu.Unlock()
	current, err := discoverSEsForSlot(ctx, m, slot)
	p.mu.Lock()
	if p.discovering[key] == ready {
		delete(p.discovering, key)
		close(ready)
	}
	if p.slotEpoch[key] != epoch {
		p.mu.Unlock()
		return nil, errSIMSlotChanged
	}
	if err != nil {
		p.mu.Unlock()
		return nil, err
	}
	if p.closed {
		p.mu.Unlock()
		return nil, errPoolClosed
	}
	if p.isRetiredLocked(m) {
		p.mu.Unlock()
		return nil, errPoolModemRetired
	}
	p.secureElems[key] = cloneSEs(current)
	p.mu.Unlock()
	return current, nil
}

func cloneSEs(values []SE) []SE {
	result := make([]SE, len(values))
	for i, value := range values {
		result[i] = value
		result[i].AID = append([]byte(nil), value.AID...)
	}
	return result
}

func poolSlots(m *modem.Modem) []uint8 {
	slot, err := modem.ActiveSIMSlot(m)
	if err != nil {
		return nil
	}
	return []uint8{slot}
}

func normalizedPoolSlots(values ...uint32) []uint8 {
	slots := make(map[uint8]struct{}, len(values))
	for _, current := range values {
		if current > 0 && current <= math.MaxUint8 {
			slots[uint8(current)] = struct{}{}
		}
	}
	result := make([]uint8, 0, len(slots))
	for slot := range slots {
		result = append(result, slot)
	}
	slices.Sort(result)
	return result
}

func (p *Pool) invalidateSIMSlots(m *modem.Modem, values ...uint32) error {
	if p == nil || m == nil {
		return nil
	}
	slots := normalizedPoolSlots(values...)
	if len(slots) == 0 {
		return nil
	}
	selected := make(map[uint8]struct{}, len(slots))
	for _, slot := range slots {
		selected[slot] = struct{}{}
	}

	p.mu.Lock()
	if p.closed || p.isRetiredLocked(m) {
		p.mu.Unlock()
		return nil
	}
	if p.slotEpoch == nil {
		p.slotEpoch = make(map[poolSEKey]uint64)
	}
	for _, slot := range slots {
		p.slotEpoch[poolSEKey{modem: m, slot: slot}]++
	}
	entries := make(map[poolKey]*poolEntry)
	for key, entry := range p.entries {
		if key.modem != m {
			continue
		}
		if _, ok := selected[key.slot]; !ok {
			continue
		}
		entries[key] = entry
		closePoolEntryLocked(entry, errSIMSlotChanged)
		delete(p.entries, key)
	}
	for key := range p.secureElems {
		if key.modem == m {
			if _, ok := selected[key.slot]; ok {
				delete(p.secureElems, key)
			}
		}
	}
	for key := range p.failures {
		if key.modem == m {
			if _, ok := selected[key.slot]; ok {
				delete(p.failures, key)
			}
		}
	}
	p.mu.Unlock()
	return p.closeEntries(entries)
}

func (p *Pool) handleModemEvent(ctx context.Context, event modem.ModemEvent) {
	if p == nil {
		return
	}
	switch event.Type {
	case modem.ModemEventAdded:
		p.warm(ctx, event.Modem)
	case modem.ModemEventChanged:
		p.retire(event.Previous)
		p.warm(ctx, event.Modem)
	case modem.ModemEventRemoved:
		p.retire(event.Modem)
	case modem.ModemEventSIMChanged:
		if err := p.invalidateSIMSlots(event.Modem, event.PreviousSIMSlot, event.SIMSlot); err != nil && event.Modem != nil {
			event.Modem.Logger().Debug("refresh LPA clients after SIM change", "error", err)
		}
		p.warm(ctx, event.Modem)
	}
}

func (p *Pool) retire(m *modem.Modem) {
	if p == nil || m == nil {
		return
	}
	p.mu.Lock()
	if p.retired == nil {
		p.retired = make(map[*modem.Modem]struct{})
	}
	p.retired[m] = struct{}{}
	entries := make(map[poolKey]*poolEntry)
	for key, entry := range p.entries {
		if key.modem == m {
			entries[key] = entry
			closePoolEntryLocked(entry, errPoolModemRetired)
			delete(p.entries, key)
		}
	}
	for key := range p.secureElems {
		if key.modem == m {
			delete(p.secureElems, key)
		}
	}
	for key := range p.failures {
		if key.modem == m {
			delete(p.failures, key)
		}
	}
	for key := range p.slotEpoch {
		if key.modem == m {
			delete(p.slotEpoch, key)
		}
	}
	for key, ready := range p.discovering {
		if key.modem == m {
			delete(p.discovering, key)
			close(ready)
		}
	}
	p.mu.Unlock()
	p.closeEntries(entries)
}

// Close releases all persistent eUICC channels. It is safe to call more than once.
func (p *Pool) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	if p.cancel != nil {
		p.cancel()
	}
	entries := maps.Clone(p.entries)
	for _, entry := range entries {
		closePoolEntryLocked(entry, errPoolClosed)
	}
	p.entries = make(map[poolKey]*poolEntry)
	for key, ready := range p.creating {
		delete(p.creating, key)
		close(ready)
	}
	for key, ready := range p.discovering {
		delete(p.discovering, key)
		close(ready)
	}
	unsubscribe := p.unsubscribe
	p.unsubscribe = nil
	p.mu.Unlock()
	if unsubscribe != nil {
		unsubscribe()
	}
	p.wg.Wait()

	p.mu.Lock()
	p.creating = make(map[poolKey]chan struct{})
	p.failures = make(map[poolKey]error)
	p.discovering = make(map[poolSEKey]chan struct{})
	p.secureElems = make(map[poolSEKey][]SE)
	p.slotEpoch = make(map[poolSEKey]uint64)
	p.retired = make(map[*modem.Modem]struct{})
	p.mu.Unlock()
	return p.closeEntries(entries)
}

func (p *Pool) isRetiredLocked(m *modem.Modem) bool {
	_, ok := p.retired[m]
	return ok
}

func closePoolEntryLocked(entry *poolEntry, err error) {
	if entry == nil {
		return
	}
	if entry.done == nil {
		entry.done = make(chan struct{})
	}
	select {
	case <-entry.done:
		return
	default:
	}
	entry.err = err
	close(entry.done)
}

func (p *Pool) closeEntries(entries map[poolKey]*poolEntry) error {
	var result error
	for _, entry := range entries {
		<-entry.gate
		if entry.lockKey != "" {
			if err := gmu.LockContext(context.Background(), entry.lockKey); err != nil {
				result = errors.Join(result, err)
				continue
			}
		}
		var closeErr error
		if errors.Is(entry.err, errSIMSlotChanged) {
			closeErr = entry.client.discard()
		} else {
			closeErr = entry.client.Close()
		}
		if closeErr != nil {
			result = errors.Join(result, closeErr)
		}
		if entry.lockKey != "" {
			gmu.Unlock(entry.lockKey)
		}
	}
	return result
}
