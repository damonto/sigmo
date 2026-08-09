package network

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"

	appconnectivity "github.com/damonto/sigmo/internal/app/connectivity"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/networkprefs"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	networkScanStatusRunning   = "running"
	networkScanStatusCompleted = "completed"
	networkScanStatusFailed    = "failed"
	networkScanStatusCanceled  = "canceled"

	networkScanCacheTTL      = 30 * time.Second
	networkScanTimeout       = 5 * time.Minute
	networkScanTaskRetention = 5 * time.Minute

	networkScanErrorCodeFailed   = "network_scan_failed"
	networkScanErrorCodeCanceled = "network_scan_canceled"
	networkScanErrorCodeTimeout  = "network_scan_timeout"
)

var (
	errNetworkScanNotFound    = errors.New("network scan not found")
	errNetworkScanStoreClosed = errors.New("network scan store is closed")
)

// NetworkScanResponse describes one modem-scoped network scan task.
//
// A scan is a resource because a full-band modem scan can take minutes. The
// response is deliberately small while the task is running; the network list
// is attached only after the task completes.
type NetworkScanResponse struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	Networks  []NetworkResponse `json:"networks,omitempty"`
	ErrorCode string            `json:"errorCode,omitempty"`
}

type network struct {
	preferences           *networkprefs.Store
	store                 *storage.Store
	scans                 *scanTaskStore
	airplaneModeLifecycle appconnectivity.AirplaneModeLifecycle
	setAirplaneMode       func(context.Context, *mmodem.Modem, bool) error
}

var errNetworkPreferencesRequired = errors.New("network preferences are required")

func newNetwork(preferences *networkprefs.Store, store *storage.Store, lifecycle appconnectivity.AirplaneModeLifecycle) (*network, error) {
	if preferences == nil {
		return nil, errNetworkPreferencesRequired
	}
	if store == nil {
		return nil, errNetworkRegistrationStorageRequired
	}
	return &network{
		preferences:           preferences,
		store:                 store,
		scans:                 newScanTaskStore(),
		airplaneModeLifecycle: lifecycle,
		setAirplaneMode: func(ctx context.Context, modem *mmodem.Modem, enabled bool) error {
			return modem.SetAirplaneMode(ctx, enabled)
		},
	}, nil
}

// scanTaskStore owns all scans started by the HTTP and MCP surfaces. The
// active map is keyed by modem generation, so two browsers cannot start two
// firmware-level scans on the same modem while different modems remain
// independent.
type scanTaskStore struct {
	mu       sync.Mutex
	active   map[*mmodem.Modem]*scanTask
	tasks    map[string]*scanTask
	cache    map[*mmodem.Modem]networkScanCache
	nextID   uint64
	now      func() time.Time
	timeout  time.Duration
	scanFunc func(context.Context, *mmodem.Modem) ([]NetworkResponse, error)
	state    func(*mmodem.Modem) networkScanState
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	closed   bool
}

type networkScanCache struct {
	scanID    string
	state     networkScanState
	expiresAt time.Time
}

type networkScanState struct {
	version        uint64
	primarySIMSlot uint32
	simIdentifier  string
	simEID         string
	simIMSI        string
	power          wwanmodem.PowerState
	simState       wwanmodem.SIMState
}

type scanTask struct {
	id        string
	modem     *mmodem.Modem
	state     networkScanState
	cancel    context.CancelFunc
	done      chan struct{}
	status    string
	networks  []NetworkResponse
	err       error
	errorCode string
	updatedAt time.Time
	finished  bool
}

func newScanTaskStore() *scanTaskStore {
	ctx, cancel := context.WithCancel(context.Background())
	return &scanTaskStore{
		active:   make(map[*mmodem.Modem]*scanTask),
		tasks:    make(map[string]*scanTask),
		cache:    make(map[*mmodem.Modem]networkScanCache),
		now:      time.Now,
		timeout:  networkScanTimeout,
		scanFunc: runNetworkScan,
		state:    currentNetworkScanState,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (s *scanTaskStore) start(modem *mmodem.Modem) (*scanTask, bool, error) {
	if modem == nil {
		return nil, false, mmodem.ErrNotFound
	}

	s.mu.Lock()
	now := s.now()
	s.cleanupLocked(now)
	if s.closed {
		s.mu.Unlock()
		return nil, false, errNetworkScanStoreClosed
	}
	state := s.state(modem)
	// A running task is shared. A canceled task keeps the protocol lease until
	// its request unwinds, so its replacement is queued behind task.done.
	var predecessor *scanTask
	if task := s.active[modem]; task != nil {
		if task.status == networkScanStatusRunning && task.state == state {
			s.mu.Unlock()
			return task, false, nil
		}
		if task.status == networkScanStatusRunning {
			s.cancelLocked(task, now)
		}
		predecessor = task
	}
	if cached, ok := s.cache[modem]; predecessor == nil && ok && now.Before(cached.expiresAt) && cached.state == state {
		if task := s.tasks[cached.scanID]; task != nil {
			s.mu.Unlock()
			return task, false, nil
		}
		delete(s.cache, modem)
	}

	task := s.startLocked(modem, state, predecessor, now)
	s.mu.Unlock()
	return task, true, nil
}

func (s *scanTaskStore) startLocked(modem *mmodem.Modem, state networkScanState, predecessor *scanTask, now time.Time) *scanTask {
	s.nextID++
	ctx, cancel := context.WithTimeout(s.ctx, s.timeout)
	task := &scanTask{
		id:        fmt.Sprintf("scan-%d", s.nextID),
		modem:     modem,
		state:     state,
		cancel:    cancel,
		done:      make(chan struct{}),
		status:    networkScanStatusRunning,
		updatedAt: now,
	}
	s.active[modem] = task
	s.tasks[task.id] = task
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(ctx, task, predecessor)
	}()
	return task
}

func currentNetworkScanState(modem *mmodem.Modem) networkScanState {
	if modem == nil {
		return networkScanState{}
	}
	snapshot := modem.Snapshot()
	state := networkScanState{
		version:        modem.NetworkStateVersion(),
		primarySIMSlot: snapshot.PrimarySIMSlot,
		power:          snapshot.Status.Power,
		simState:       snapshot.Status.SIM,
	}
	if snapshot.SIM != nil {
		state.simIdentifier = snapshot.SIM.Identifier
		state.simEID = snapshot.SIM.Eid
		state.simIMSI = snapshot.SIM.Imsi
	}
	return state
}

func (s *scanTaskStore) run(ctx context.Context, task, predecessor *scanTask) {
	defer task.cancel()
	modemDone := task.modem.Done()
	select {
	case <-modemDone:
		task.cancel()
	default:
	}
	watchDone := make(chan struct{})
	watchStopped := make(chan struct{})
	go func() {
		defer close(watchStopped)
		select {
		case <-modemDone:
			task.cancel()
		case <-watchDone:
		}
	}()
	defer func() {
		close(watchDone)
		<-watchStopped
	}()

	if predecessor != nil {
		select {
		case <-predecessor.done:
		case <-ctx.Done():
			err := ctx.Err()
			s.mu.Lock()
			s.stopLocked(task, err, s.now())
			s.mu.Unlock()
			// The task never acquired a client, but its predecessor may still own
			// one. Keep this task's done channel open so later replacements remain
			// serialized behind the real protocol lease.
			<-predecessor.done
			s.finish(task, nil, err)
			return
		}
	}
	if err := ctx.Err(); err != nil {
		s.finish(task, nil, err)
		return
	}
	networks, err := s.scanFunc(ctx, task.modem)
	ctxErr := ctx.Err()
	if ctxErr == nil {
		select {
		case <-modemDone:
			ctxErr = context.Canceled
		default:
		}
	}
	if ctxErr != nil {
		// A modem replacement can cancel the task context even if a backend
		// returns a partial result or a protocol-specific closed error.
		err = ctxErr
	}
	s.finish(task, networks, err)
}

func (s *scanTaskStore) finish(task *scanTask, networks []NetworkResponse, err error) {
	s.mu.Lock()
	if task.status == networkScanStatusRunning {
		switch {
		case err == nil:
			task.status = networkScanStatusCompleted
			task.networks = cloneNetworks(networks)
			if !s.closed && s.state(task.modem) == task.state {
				s.cache[task.modem] = networkScanCache{
					scanID:    task.id,
					state:     task.state,
					expiresAt: s.now().Add(networkScanCacheTTL),
				}
			}
		default:
			s.stopLocked(task, err, s.now())
		}
	}
	task.updatedAt = s.now()
	task.finished = true
	if s.active[task.modem] == task {
		delete(s.active, task.modem)
	}
	close(task.done)
	status := task.status
	taskErr := task.err
	s.mu.Unlock()

	if status == networkScanStatusFailed {
		slog.Warn("network scan stopped", "scan_id", task.id, "imei", task.modem.EquipmentIdentifier, "error", taskErr)
	}
}

func (s *scanTaskStore) get(modem *mmodem.Modem, id string) (NetworkScanResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(s.now())
	task := s.tasks[id]
	if task == nil || task.modem != modem {
		return NetworkScanResponse{}, errNetworkScanNotFound
	}
	return task.responseLocked(), nil
}

func (s *scanTaskStore) wait(ctx context.Context, task *scanTask) (NetworkScanResponse, error) {
	select {
	case <-task.done:
	case <-ctx.Done():
		return NetworkScanResponse{}, ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	response := task.responseLocked()
	if task.status == networkScanStatusCompleted {
		return response, nil
	}
	if task.err != nil {
		return response, task.err
	}
	return response, errors.New("network scan ended without a result")
}

func (s *scanTaskStore) cancelTask(modem *mmodem.Modem, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(s.now())
	task := s.tasks[id]
	if task == nil || task.modem != modem {
		return errNetworkScanNotFound
	}
	s.cancelLocked(task, s.now())
	return nil
}

// invalidate discards results after a radio setting or registration change.
// An in-flight scan is canceled too, because its result would describe the
// modem state from before the write completed.
func (s *scanTaskStore) invalidate(modem *mmodem.Modem) {
	if modem == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, modem)
	if task := s.active[modem]; task != nil && task.status == networkScanStatusRunning {
		s.cancelLocked(task, s.now())
	}
}

func (s *scanTaskStore) cancelLocked(task *scanTask, now time.Time) {
	if task.status != networkScanStatusRunning {
		return
	}
	s.stopLocked(task, context.Canceled, now)
	task.cancel()
}

func (s *scanTaskStore) stopLocked(task *scanTask, err error, now time.Time) {
	if task.status != networkScanStatusRunning {
		return
	}
	switch {
	case errors.Is(err, context.Canceled):
		task.status = networkScanStatusCanceled
		task.err = context.Canceled
		task.errorCode = networkScanErrorCodeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		task.status = networkScanStatusFailed
		task.err = context.DeadlineExceeded
		task.errorCode = networkScanErrorCodeTimeout
	default:
		task.status = networkScanStatusFailed
		task.err = err
		task.errorCode = networkScanErrorCodeFailed
	}
	task.updatedAt = now
}

func (s *scanTaskStore) cleanupLocked(now time.Time) {
	for modem, cached := range s.cache {
		if !now.Before(cached.expiresAt) {
			delete(s.cache, modem)
		}
	}
	for id, task := range s.tasks {
		if !task.finished {
			continue
		}
		if task.status != networkScanStatusRunning && now.Sub(task.updatedAt) >= networkScanTaskRetention {
			delete(s.tasks, id)
		}
	}
}

func (s *scanTaskStore) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.cancel()
	now := s.now()
	for _, task := range s.active {
		s.cancelLocked(task, now)
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (t *scanTask) responseLocked() NetworkScanResponse {
	return NetworkScanResponse{
		ID:        t.id,
		Status:    t.status,
		Networks:  cloneNetworks(t.networks),
		ErrorCode: t.errorCode,
	}
}

func cloneNetworks(networks []NetworkResponse) []NetworkResponse {
	if networks == nil {
		return nil
	}
	result := slices.Clone(networks)
	for i := range result {
		result[i].AccessTechnologies = slices.Clone(result[i].AccessTechnologies)
	}
	return result
}

func runNetworkScan(ctx context.Context, modem *mmodem.Modem) ([]NetworkResponse, error) {
	networks, err := modem.ThreeGPP().ScanNetworks(ctx)
	if err != nil {
		return nil, fmt.Errorf("read visible networks: %w", err)
	}

	response := make([]NetworkResponse, 0, len(networks))
	for _, network := range networks {
		response = append(response, NetworkResponse{
			Status:             networkAvailabilityName(network),
			OperatorName:       network.Name,
			OperatorShortName:  network.Name,
			OperatorCode:       network.ID,
			AccessTechnologies: accessTechnologyStrings(network.Technology),
		})
	}
	return response, nil
}

func (n *network) List(ctx context.Context, modem *mmodem.Modem) ([]NetworkResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	task, _, err := n.scans.start(modem)
	if err != nil {
		return nil, fmt.Errorf("start network scan: %w", err)
	}
	response, err := n.scans.wait(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("scan networks: %w", err)
	}
	return response.Networks, nil
}

func (n *network) StartScan(ctx context.Context, modem *mmodem.Modem) (NetworkScanResponse, bool, error) {
	if err := ctx.Err(); err != nil {
		return NetworkScanResponse{}, false, err
	}
	task, created, err := n.scans.start(modem)
	if err != nil {
		return NetworkScanResponse{}, false, err
	}
	return n.scans.response(task), created, nil
}

func (n *network) Scan(ctx context.Context, modem *mmodem.Modem, id string) (NetworkScanResponse, error) {
	if err := ctx.Err(); err != nil {
		return NetworkScanResponse{}, err
	}
	return n.scans.get(modem, id)
}

func (n *network) InvalidateScan(modem *mmodem.Modem) {
	n.scans.invalidate(modem)
}

func (n *network) Close() {
	if n != nil && n.scans != nil {
		n.scans.close()
	}
}

func (s *scanTaskStore) response(task *scanTask) NetworkScanResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return task.responseLocked()
}
