package network

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestScanTaskStoreSharesActiveScanAndCachesResult(t *testing.T) {
	store := newTestScanTaskStore(t)
	modem := &mmodem.Modem{}
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	store.scanFunc = func(ctx context.Context, _ *mmodem.Modem) ([]NetworkResponse, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return []NetworkResponse{{OperatorCode: "00101"}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	first, created, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if !created {
		t.Fatal("first start created = false, want true")
	}
	<-started

	second, created, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("second start() error = %v", err)
	}
	if created || second != first {
		t.Fatalf("second start = (%p, %t), want (%p, false)", second, created, first)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("scan calls = %d, want 1", got)
	}

	close(release)
	response, err := store.wait(t.Context(), first)
	if err != nil {
		t.Fatalf("wait() error = %v", err)
	}
	if response.Status != networkScanStatusCompleted || len(response.Networks) != 1 {
		t.Fatalf("response = %+v, want completed result", response)
	}

	cached, created, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("cached start() error = %v", err)
	}
	if created || cached.id != first.id {
		t.Fatalf("cached start = (%s, %t), want (%s, false)", cached.id, created, first.id)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("cached scan calls = %d, want 1", got)
	}
}

func TestScanTaskStoreCancelAndModemClose(t *testing.T) {
	tests := []struct {
		name  string
		close bool
	}{
		{name: "explicit cancellation"},
		{name: "modem close", close: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestScanTaskStore(t)
			modem := &mmodem.Modem{}
			store.scanFunc = func(ctx context.Context, _ *mmodem.Modem) ([]NetworkResponse, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			task, _, err := store.start(t.Context(), modem)
			if err != nil {
				t.Fatalf("start() error = %v", err)
			}

			if tt.close {
				if err := modem.Close(); err != nil {
					t.Fatalf("Close() error = %v", err)
				}
			} else if err := store.cancelTask(modem, task.id); err != nil {
				t.Fatalf("cancelTask() error = %v", err)
			}

			response, err := store.wait(t.Context(), task)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("wait() error = %v, want context.Canceled", err)
			}
			if response.Status != networkScanStatusCanceled {
				t.Fatalf("response status = %q, want canceled", response.Status)
			}
		})
	}
}

func TestScanTaskStoreInvalidatesCache(t *testing.T) {
	store := newTestScanTaskStore(t)
	store.scanFunc = func(context.Context, *mmodem.Modem) ([]NetworkResponse, error) {
		return []NetworkResponse{{OperatorCode: "00101"}}, nil
	}
	modem := &mmodem.Modem{}

	first, _, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if _, err := store.wait(t.Context(), first); err != nil {
		t.Fatalf("wait() error = %v", err)
	}

	store.invalidate(modem)
	second, created, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("start() after invalidate error = %v", err)
	}
	if !created || second.id == first.id {
		t.Fatalf("start() after invalidate = (%s, %t), want a new task", second.id, created)
	}
	if _, err := store.wait(t.Context(), second); err != nil {
		t.Fatalf("wait() after invalidate error = %v", err)
	}
}

func TestScanTaskStoreCleansExpiredEntries(t *testing.T) {
	store := newTestScanTaskStore(t)
	now := time.Now()
	store.now = func() time.Time { return now }
	store.scanFunc = func(context.Context, *mmodem.Modem) ([]NetworkResponse, error) { return nil, nil }
	modem := &mmodem.Modem{}
	task, _, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if _, err := store.wait(t.Context(), task); err != nil {
		t.Fatalf("wait() error = %v", err)
	}

	now = now.Add(networkScanTaskRetention + time.Second)
	if _, err := store.get(modem, task.id); !errors.Is(err, errNetworkScanNotFound) {
		t.Fatalf("get() error = %v, want not found after retention", err)
	}
}

func TestScanTaskStoreQueuesReplacementUntilCanceledTaskReleasesClient(t *testing.T) {
	store := newTestScanTaskStore(t)
	modem := &mmodem.Modem{}
	firstStarted := make(chan struct{})
	firstCanceled := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	var releaseOnce atomic.Bool
	t.Cleanup(func() {
		if releaseOnce.CompareAndSwap(false, true) {
			close(releaseFirst)
		}
	})

	var calls atomic.Int32
	store.scanFunc = func(ctx context.Context, _ *mmodem.Modem) ([]NetworkResponse, error) {
		switch calls.Add(1) {
		case 1:
			close(firstStarted)
			<-ctx.Done()
			close(firstCanceled)
			<-releaseFirst
			return nil, ctx.Err()
		case 2:
			close(secondStarted)
			return []NetworkResponse{{OperatorCode: "00102"}}, nil
		default:
			return nil, errors.New("unexpected scan call")
		}
	}

	first, _, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("first start() error = %v", err)
	}
	<-firstStarted
	if err := store.cancelTask(modem, first.id); err != nil {
		t.Fatalf("cancelTask() error = %v", err)
	}
	<-firstCanceled

	second, created, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("replacement start() error = %v", err)
	}
	if !created || second == first {
		t.Fatalf("replacement start = (%p, %t), want a new task", second, created)
	}
	if second.status != networkScanStatusRunning {
		t.Fatalf("replacement status = %q, want running", second.status)
	}
	select {
	case <-secondStarted:
		t.Fatal("replacement acquired the protocol client before predecessor finished")
	default:
	}

	if releaseOnce.CompareAndSwap(false, true) {
		close(releaseFirst)
	}
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("replacement did not start after predecessor released the client")
	}
	response, err := store.wait(t.Context(), second)
	if err != nil {
		t.Fatalf("replacement wait() error = %v", err)
	}
	if response.Status != networkScanStatusCompleted || len(response.Networks) != 1 {
		t.Fatalf("replacement response = %+v, want completed result", response)
	}
}

func TestScanTaskStoreKeepsLeaseChainAfterQueuedReplacementTimesOut(t *testing.T) {
	store := newTestScanTaskStore(t)
	store.timeout = 30 * time.Millisecond
	modem := &mmodem.Modem{}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	thirdStarted := make(chan struct{})
	var releaseOnce atomic.Bool
	t.Cleanup(func() {
		if releaseOnce.CompareAndSwap(false, true) {
			close(releaseFirst)
		}
	})

	var calls atomic.Int32
	store.scanFunc = func(ctx context.Context, _ *mmodem.Modem) ([]NetworkResponse, error) {
		switch calls.Add(1) {
		case 1:
			close(firstStarted)
			<-ctx.Done()
			<-releaseFirst
			return nil, ctx.Err()
		case 2:
			close(thirdStarted)
			return nil, nil
		default:
			return nil, errors.New("unexpected scan call")
		}
	}

	first, _, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("first start() error = %v", err)
	}
	<-firstStarted
	if err := store.cancelTask(modem, first.id); err != nil {
		t.Fatalf("cancelTask() error = %v", err)
	}
	second, _, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("second start() error = %v", err)
	}
	waitForScanStatus(t, store, modem, second.id, networkScanStatusFailed)

	store.timeout = time.Second
	third, created, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("third start() error = %v", err)
	}
	if !created || third == second {
		t.Fatalf("third start = (%p, %t), want new queued task", third, created)
	}
	select {
	case <-thirdStarted:
		t.Fatal("third task bypassed the still-owned protocol lease")
	default:
	}

	if releaseOnce.CompareAndSwap(false, true) {
		close(releaseFirst)
	}
	select {
	case <-thirdStarted:
	case <-time.After(time.Second):
		t.Fatal("third task did not start after the lease chain finished")
	}
	if _, err := store.wait(t.Context(), third); err != nil {
		t.Fatalf("third wait() error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("scan calls = %d, want first and third only", got)
	}
}

func TestScanTaskStoreCacheRequiresSameNetworkState(t *testing.T) {
	store := newTestScanTaskStore(t)
	modem := &mmodem.Modem{}
	var stateVersion atomic.Uint64
	stateVersion.Store(1)
	store.state = func(*mmodem.Modem) networkScanState {
		return networkScanState{version: stateVersion.Load()}
	}
	var calls atomic.Int32
	store.scanFunc = func(context.Context, *mmodem.Modem) ([]NetworkResponse, error) {
		calls.Add(1)
		return nil, nil
	}

	first, _, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("first start() error = %v", err)
	}
	if _, err := store.wait(t.Context(), first); err != nil {
		t.Fatalf("first wait() error = %v", err)
	}

	stateVersion.Store(2)
	second, created, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("start() after state change error = %v", err)
	}
	if !created || second.id == first.id {
		t.Fatalf("start() after state change = (%s, %t), want new task", second.id, created)
	}
	if _, err := store.wait(t.Context(), second); err != nil {
		t.Fatalf("second wait() error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("scan calls = %d, want 2", got)
	}
}

func TestScanTaskStoreTimesOutScan(t *testing.T) {
	store := newTestScanTaskStore(t)
	store.timeout = 20 * time.Millisecond
	store.scanFunc = func(ctx context.Context, _ *mmodem.Modem) ([]NetworkResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	task, _, err := store.start(t.Context(), &mmodem.Modem{})
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	response, err := store.wait(t.Context(), task)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait() error = %v, want context deadline", err)
	}
	if response.Status != networkScanStatusFailed || response.ErrorCode != networkScanErrorCodeTimeout {
		t.Fatalf("response = %+v, want timeout failure", response)
	}
}

func TestScanTaskStoreCloseCancelsTasksAndRejectsStarts(t *testing.T) {
	store := newScanTaskStore()
	started := make(chan struct{})
	store.scanFunc = func(ctx context.Context, _ *mmodem.Modem) ([]NetworkResponse, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	modem := &mmodem.Modem{}
	task, _, err := store.start(t.Context(), modem)
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	<-started
	store.close()

	response, err := store.wait(t.Context(), task)
	if !errors.Is(err, context.Canceled) || response.Status != networkScanStatusCanceled {
		t.Fatalf("wait() = (%+v, %v), want canceled", response, err)
	}
	if _, _, err := store.start(t.Context(), modem); !errors.Is(err, errNetworkScanStoreClosed) {
		t.Fatalf("start() after close error = %v, want store closed", err)
	}
}

func newTestScanTaskStore(t *testing.T) *scanTaskStore {
	t.Helper()
	store := newScanTaskStore()
	t.Cleanup(store.close)
	return store
}

func waitForScanStatus(t *testing.T, store *scanTaskStore, modem *mmodem.Modem, id, want string) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		response, err := store.get(modem, id)
		if err != nil {
			t.Fatalf("get(%q) error = %v", id, err)
		}
		if response.Status == want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("scan %q status = %q, want %q", id, response.Status, want)
		case <-ticker.C:
		}
	}
}
