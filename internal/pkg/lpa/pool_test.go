package lpa

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	euicclpa "github.com/damonto/euicc-go/lpa"
	"github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestLeaseSerializesAPDUOperations(t *testing.T) {
	entry := &poolEntry{client: &LPA{}, gate: make(chan struct{}, 1)}
	entry.gate <- struct{}{}

	first, err := lease(context.Background(), entry)
	if err != nil {
		t.Fatalf("first lease: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := lease(ctx, entry); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second lease error = %v, want deadline exceeded", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("release first lease: %v", err)
	}
	second, err := lease(context.Background(), entry)
	if err != nil {
		t.Fatalf("second lease after release: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("release second lease: %v", err)
	}
}

func TestLeaseReservesSIMSlotUntilClose(t *testing.T) {
	m := new(modem.Modem)
	entry := &poolEntry{modem: m, client: &LPA{}, gate: make(chan struct{}, 1)}
	entry.gate <- struct{}{}

	client, err := lease(t.Context(), entry)
	if err != nil {
		t.Fatalf("lease() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := m.ReserveSIMSlot(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ReserveSIMSlot() during lease error = %v, want %v", err, context.DeadlineExceeded)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("release lease: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second release lease: %v", err)
	}

	release, err := m.ReserveSIMSlot(t.Context())
	if err != nil {
		t.Fatalf("ReserveSIMSlot() after lease error = %v", err)
	}
	release()
}

func TestLeaseCancellationReleasesGateAndSIMReservation(t *testing.T) {
	m := new(modem.Modem)
	key := "test:lease-cancellation"
	gmu.Lock(key)
	locked := true
	t.Cleanup(func() {
		if locked {
			gmu.Unlock(key)
		}
	})
	entry := &poolEntry{
		modem:   m,
		client:  &LPA{},
		gate:    make(chan struct{}, 1),
		lockKey: key,
	}
	entry.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := lease(ctx, entry); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lease() error = %v, want %v", err, context.DeadlineExceeded)
	}

	gmu.Unlock(key)
	locked = false
	client, err := lease(t.Context(), entry)
	if err != nil {
		t.Fatalf("lease() after cancellation error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("release lease: %v", err)
	}
}

func TestLeaseBindsCallerContextForPersistentClient(t *testing.T) {
	base := context.Background()
	operation := newOperationContext(base)
	entry := &poolEntry{
		client: &LPA{operation: operation},
		gate:   make(chan struct{}, 1),
	}
	entry.gate <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())

	client, err := lease(ctx, entry)
	if err != nil {
		t.Fatalf("lease() error = %v", err)
	}
	cancel()
	if !errors.Is(operation.context().Err(), context.Canceled) {
		t.Fatalf("operation context error = %v, want %v", operation.context().Err(), context.Canceled)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("release lease: %v", err)
	}
	if operation.context() != base {
		t.Fatal("operation context was not reset to the pool lifetime context")
	}
}

func TestLeaseCloseForRefreshRetiresAndClosesPersistentClient(t *testing.T) {
	var closes atomic.Int32
	key := poolKey{}
	pool := &Pool{entries: make(map[poolKey]*poolEntry)}
	entry := &poolEntry{
		pool: pool,
		key:  key,
		client: &LPA{close: func() error {
			closes.Add(1)
			return nil
		}},
		gate: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	entry.gate <- struct{}{}
	pool.entries[key] = entry

	client, err := lease(t.Context(), entry)
	if err != nil {
		t.Fatalf("lease() error = %v", err)
	}
	if err := client.CloseForRefresh(); err != nil {
		t.Fatalf("CloseForRefresh() error = %v", err)
	}
	if got := closes.Load(); got != 1 {
		t.Fatalf("persistent client close calls = %d, want 1", got)
	}
	if pool.entries[key] != nil {
		t.Fatal("retired entry remains in pool")
	}
	if _, err := lease(t.Context(), entry); !errors.Is(err, errPoolEntryRetired) {
		t.Fatalf("lease() after refresh close error = %v, want %v", err, errPoolEntryRetired)
	}
}

func TestLeaseRejectsRetiredEntry(t *testing.T) {
	entryErr := errors.New("entry retired")
	entry := &poolEntry{
		client: &LPA{},
		gate:   make(chan struct{}, 1),
		done:   make(chan struct{}),
		err:    entryErr,
	}
	entry.gate <- struct{}{}
	close(entry.done)

	if _, err := lease(context.Background(), entry); !errors.Is(err, entryErr) {
		t.Fatalf("lease() error = %v, want %v", err, entryErr)
	}
}

func TestPoolCloseClosesPersistentClientOnce(t *testing.T) {
	var closes atomic.Int32
	entry := &poolEntry{
		client: &LPA{close: func() error {
			closes.Add(1)
			return nil
		}},
		gate: make(chan struct{}, 1),
	}
	entry.gate <- struct{}{}
	p := &Pool{
		ctx:         context.Background(),
		cancel:      func() {},
		entries:     map[poolKey]*poolEntry{{}: entry},
		creating:    make(map[poolKey]chan struct{}),
		failures:    make(map[poolKey]error),
		secureElems: make(map[poolSEKey][]SE),
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Pool.Close() error = %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Pool.Close() error = %v", err)
	}
	if got := closes.Load(); got != 1 {
		t.Fatalf("client close calls = %d, want 1", got)
	}
}

func TestPoolRoutesDefaultSEOnlyToActiveSlot(t *testing.T) {
	m := &modem.Modem{
		EquipmentIdentifier: "test-modem",
		PrimarySimSlot:      2,
		SimSlots:            []uint32{1, 2},
	}
	wantClient := &LPA{Client: &euicclpa.Client{}}
	key := poolKey{modem: m, slot: 2, seID: SEIDDefault}
	entry := &poolEntry{client: wantClient, gate: make(chan struct{}, 1), done: make(chan struct{})}
	entry.gate <- struct{}{}
	p := &Pool{
		ctx:      context.Background(),
		entries:  map[poolKey]*poolEntry{key: entry},
		creating: make(map[poolKey]chan struct{}),
		failures: map[poolKey]error{
			{modem: m, slot: 1, seID: SEIDDefault}: ErrNoSupportedAID,
		},
		discovering: make(map[poolSEKey]chan struct{}),
		secureElems: map[poolSEKey][]SE{
			{modem: m, slot: 1}: {DefaultSE},
			{modem: m, slot: 2}: {DefaultSE},
		},
		retired: make(map[*modem.Modem]struct{}),
	}

	ses, err := p.SecureElements(t.Context(), m)
	if err != nil {
		t.Fatalf("SecureElements() error = %v", err)
	}
	if len(ses) != 1 || ses[0].ID != SEIDDefault {
		t.Fatalf("SecureElements() = %#v, want one default SE", ses)
	}

	client, err := p.Acquire(t.Context(), m, SEIDDefault)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if client.Client != wantClient.Client {
		t.Fatal("Acquire() did not lease the active slot 2 client")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("release client: %v", err)
	}
}

func TestExposeTargetsDisambiguatesDuplicateSEIDs(t *testing.T) {
	targets := exposeTargets([]poolTarget{
		{se: DefaultSE, slot: 1, sourceID: SEIDDefault},
		{se: DefaultSE, slot: 2, sourceID: SEIDDefault},
	})

	if got := targets[0].se.ID; got != SEIDDefault {
		t.Fatalf("first SE ID = %q, want %q", got, SEIDDefault)
	}
	if got := targets[1].se.ID; got != "default-slot2" {
		t.Fatalf("second SE ID = %q, want %q", got, "default-slot2")
	}
	if targets[0].se.Label == targets[1].se.Label {
		t.Fatalf("duplicate labels were not disambiguated: %q", targets[0].se.Label)
	}
}

func TestPoolSlotsUsesOnlyActiveSlot(t *testing.T) {
	m := &modem.Modem{
		PrimarySimSlot: 2,
		SimSlots:       []uint32{1, 2},
	}

	got := poolSlots(m)
	want := []uint8{2}
	if !slices.Equal(got, want) {
		t.Fatalf("poolSlots() = %v, want %v", got, want)
	}
}

func TestWarmableEUICCState(t *testing.T) {
	tests := []struct {
		name        string
		snapshot    modem.ModemSnapshot
		wantReady   bool
		wantSettled bool
	}{
		{name: "classification pending", snapshot: modem.ModemSnapshot{Status: wwanmodem.Status{SIM: wwanmodem.SIMStateReady}, SIM: &modem.SIM{Identifier: "8901"}}},
		{name: "eUICC initializing", snapshot: modem.ModemSnapshot{SIM: &modem.SIM{Identifier: "8901", ATR: []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}}}},
		{name: "eUICC ready", snapshot: modem.ModemSnapshot{Status: wwanmodem.Status{SIM: wwanmodem.SIMStateReady}, SIM: &modem.SIM{Identifier: "8901", ATR: []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}}}, wantReady: true, wantSettled: true},
		{name: "physical SIM", snapshot: modem.ModemSnapshot{Status: wwanmodem.Status{SIM: wwanmodem.SIMStateReady}, SIM: &modem.SIM{Identifier: "8901", ATR: []byte{0x3B, 0x00}}}, wantSettled: true},
		{name: "SIM absent", snapshot: modem.ModemSnapshot{Status: wwanmodem.Status{SIM: wwanmodem.SIMStateAbsent}}, wantSettled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotReady, gotSettled := warmableEUICCState(tt.snapshot)
			if gotReady != tt.wantReady || gotSettled != tt.wantSettled {
				t.Fatalf("warmableEUICCState() = %t, %t, want %t, %t", gotReady, gotSettled, tt.wantReady, tt.wantSettled)
			}
		})
	}
}

func TestInvalidateSIMSlotsClosesOnlyAffectedEntries(t *testing.T) {
	m := &modem.Modem{EquipmentIdentifier: "test-modem"}
	var slot1Closes atomic.Int32
	var slot2Closes atomic.Int32
	newEntry := func(closes *atomic.Int32) *poolEntry {
		entry := &poolEntry{
			client: &LPA{close: func() error {
				closes.Add(1)
				return nil
			}},
			gate: make(chan struct{}, 1),
			done: make(chan struct{}),
		}
		entry.gate <- struct{}{}
		return entry
	}
	key1 := poolKey{modem: m, slot: 1, seID: SEIDDefault}
	key2 := poolKey{modem: m, slot: 2, seID: SEIDDefault}
	p := &Pool{
		entries: map[poolKey]*poolEntry{
			key1: newEntry(&slot1Closes),
			key2: newEntry(&slot2Closes),
		},
		failures: map[poolKey]error{
			key1: ErrNoSupportedAID,
			key2: ErrNoSupportedAID,
		},
		secureElems: map[poolSEKey][]SE{
			{modem: m, slot: 1}: {DefaultSE},
			{modem: m, slot: 2}: {DefaultSE},
		},
		retired: make(map[*modem.Modem]struct{}),
	}

	if err := p.invalidateSIMSlots(m, 1); err != nil {
		t.Fatalf("invalidateSIMSlots() error = %v", err)
	}
	if got := slot1Closes.Load(); got != 1 {
		t.Fatalf("slot 1 close calls = %d, want 1", got)
	}
	if got := slot2Closes.Load(); got != 0 {
		t.Fatalf("slot 2 close calls = %d, want 0", got)
	}
	if p.entries[key1] != nil || p.entries[key2] == nil {
		t.Fatalf("entries after invalidation = %#v, want only slot 2", p.entries)
	}
	if _, ok := p.secureElems[poolSEKey{modem: m, slot: 1}]; ok {
		t.Fatal("slot 1 secure element cache was not cleared")
	}
	if _, ok := p.secureElems[poolSEKey{modem: m, slot: 2}]; !ok {
		t.Fatal("slot 2 secure element cache was cleared")
	}
	if _, ok := p.failures[key1]; ok {
		t.Fatal("slot 1 failure cache was not cleared")
	}
	if _, ok := p.failures[key2]; !ok {
		t.Fatal("slot 2 failure cache was cleared")
	}
	if got := p.slotEpoch[poolSEKey{modem: m, slot: 1}]; got != 1 {
		t.Fatalf("slot 1 epoch = %d, want 1", got)
	}
}
