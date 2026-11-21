package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, profile := range []storage.SIMProfile{
		{ProfileID: "line", ModemID: "modem", Kind: "esim", SEID: "se"},
		{ProfileID: "other", ModemID: "modem", Kind: "esim", SEID: "se"},
	} {
		if err := db.RecordSIMProfile(t.Context(), profile); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

type fakeExecutor struct {
	ran          chan storage.SIMAutomation
	restoreCalls int
	onExecute    func(context.Context)
	onRestore    func(context.Context) (string, bool)
}

func (*fakeExecutor) bind(context.Context, storage.SIMAutomation) (string, error) { return "eid", nil }
func (e *fakeExecutor) execute(ctx context.Context, task storage.SIMAutomation, _ Action, _ func(context.Context, restoration) error) outcome {
	select {
	case e.ran <- task:
		if e.onExecute != nil {
			e.onExecute(ctx)
		}
		return outcome{Status: statusSucceeded}
	case <-ctx.Done():
		return outcome{Status: "interrupted"}
	}
}
func (e *fakeExecutor) restore(ctx context.Context, _ storage.SIMAutomation, _ restoration) (string, bool) {
	e.restoreCalls++
	if e.onRestore != nil {
		return e.onRestore(ctx)
	}
	return "restored", false
}

func saveTestTask(t *testing.T, db *storage.Store, id, profile string, at time.Time, repeatDays int64) storage.SIMAutomation {
	t.Helper()
	record, err := db.CreateSIMAutomation(t.Context(), storage.SIMAutomation{
		ID: id, ProfileID: profile, ModemID: "modem", SEID: "se", EID: "eid", Action: `{"type":"data","bytes":1024}`,
		ScheduledAt: at.UnixMilli(), NextAt: at.UnixMilli(), RepeatDays: repeatDays,
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestSchedulerCatchesUpOnceAndRestoresInterruptedTask(t *testing.T) {
	db := testStore(t)
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	due := saveTestTask(t, db, "repeat", "line", now.Add(-10*24*time.Hour-time.Hour), 3)
	interrupted := saveTestTask(t, db, "interrupted", "other", now.Add(-time.Hour), 0)
	claimed, ok, err := db.ClaimSIMAutomation(t.Context(), interrupted, interrupted.NextAt, interrupted.NextAt, 0)
	if err != nil || !ok {
		t.Fatalf("initial claim = %v, %v", ok, err)
	}
	if err := db.SetSIMAutomationRestore(t.Context(), claimed, `{"Previous":"original","Slot":1}`); err != nil {
		t.Fatal(err)
	}

	exec := &fakeExecutor{ran: make(chan storage.SIMAutomation, 2)}
	s := &Service{store: db, executor: exec, wake: make(chan struct{}, 1), now: func() time.Time { return now }}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case got := <-exec.ran:
		if got.ID != due.ID || got.RunningAt != now.Add(-25*time.Hour).UnixMilli() || got.NextAt != now.Add(47*time.Hour).UnixMilli() {
			t.Fatalf("caught-up task = %+v", got)
		}
	case err := <-done:
		t.Fatalf("scheduler stopped: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler never ran the missed occurrence")
	}
	waitForTask(t, s, "line", "succeeded")
	recovered := waitForTask(t, s, "other", "interrupted")
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if recovered.LastRun.RestoreCode != "restored" || recovered.Restoring || recovered.NextAt != nil || exec.restoreCalls != 1 {
		t.Fatalf("interrupted task = %+v, restores = %d", recovered, exec.restoreCalls)
	}
	select {
	case work := <-exec.ran:
		t.Fatalf("replayed task %+v", work)
	default:
	}
}

func TestSchedulerKeepsFutureDeadlinesWithOverdueWork(t *testing.T) {
	db := testStore(t)
	now := time.Now()
	saveTestTask(t, db, "running", "line", now.Add(-2*time.Minute), 0)
	saveTestTask(t, db, "waiting", "other", now.Add(-time.Minute), 0)
	if err := db.RecordSIMProfile(t.Context(), storage.SIMProfile{
		ProfileID: "travel", ModemID: "another", Kind: "esim", SEID: "se",
	}); err != nil {
		t.Fatal(err)
	}
	exec := &fakeExecutor{
		ran:       make(chan storage.SIMAutomation, 2),
		onExecute: func(ctx context.Context) { <-ctx.Done() },
	}
	s := &Service{store: db, executor: exec, wake: make(chan struct{}, 1), now: time.Now}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("Run() error = %v, want context canceled", err)
		}
	})
	expectRun := func(id string) {
		t.Helper()
		select {
		case task := <-exec.ran:
			if task.ID != id {
				t.Fatalf("ran task %s, want %s", task.ID, id)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("did not run task %s", id)
		}
	}
	expectRun("running")
	at := time.Now().Add(time.Second).UnixMilli()
	if _, err := db.CreateSIMAutomation(t.Context(), storage.SIMAutomation{
		ID: "future", ProfileID: "travel", ModemID: "another", SEID: "se", EID: "eid",
		Action: `{"type":"data","bytes":1024}`, ScheduledAt: at, NextAt: at,
	}); err != nil {
		t.Fatal(err)
	}
	s.signal()
	expectRun("future")
}

func TestSchedulerRetriesRestorationInterruptedByShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	exec := &fakeExecutor{}
	exec.onRestore = func(cleanupCtx context.Context) (string, bool) {
		if exec.restoreCalls == 1 {
			cancel()
			return "restore_failed", true
		}
		if err := cleanupCtx.Err(); err != nil {
			t.Errorf("restoration retry context = %v, want active", err)
		}
		if _, ok := cleanupCtx.Deadline(); !ok {
			t.Error("restoration retry has no deadline")
		}
		return "restored", false
	}
	s := &Service{executor: exec}
	state, code := s.restore(ctx, storage.SIMAutomation{}, `{"Previous":"original","Slot":1}`)
	if state != "" || code != "restored" || exec.restoreCalls != 2 {
		t.Fatalf("restore() = (%q, %q), attempts = %d", state, code, exec.restoreCalls)
	}
}

func TestSchedulerJoinsWorkerResultsOnShutdown(t *testing.T) {
	for _, tt := range []struct {
		name       string
		closeStore bool
	}{
		{name: "completed action is recorded"},
		{name: "persistence error is returned", closeStore: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := testStore(t)
			now := time.Now()
			saveTestTask(t, db, "once", "line", now.Add(-time.Minute), 0)
			exec := &fakeExecutor{
				ran: make(chan storage.SIMAutomation, 1),
				onExecute: func(ctx context.Context) {
					<-ctx.Done()
					if tt.closeStore {
						if err := db.Close(); err != nil {
							t.Errorf("close store: %v", err)
						}
					}
				},
			}
			s := &Service{store: db, executor: exec, wake: make(chan struct{}, 1), now: func() time.Time { return now }}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- s.Run(ctx) }()
			select {
			case <-exec.ran:
				cancel()
			case <-ctx.Done():
				t.Fatal("automation did not start")
			}
			select {
			case err := <-done:
				if tt.closeStore {
					if err == nil || !strings.Contains(err.Error(), "save SIM automation execution") {
						t.Fatalf("Run() error = %v, want result persistence error", err)
					}
					return
				}
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Run() error = %v, want context canceled", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("scheduler did not join its worker")
			}
			waitForTask(t, s, "line", statusSucceeded)
		})
	}
}

func TestInvalidStoredActionDoesNotRun(t *testing.T) {
	db := testStore(t)
	now := time.Now()
	record, err := db.CreateSIMAutomation(t.Context(), storage.SIMAutomation{
		ID: "invalid", ProfileID: "line", ModemID: "modem", SEID: "se",
		Action: `{"type":"sms"}`, ScheduledAt: now.UnixMilli(), NextAt: now.UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := db.ClaimSIMAutomation(t.Context(), record, record.NextAt, record.NextAt, 0)
	if err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	exec := &fakeExecutor{ran: make(chan storage.SIMAutomation, 1)}
	s := &Service{store: db, executor: exec, now: time.Now}
	if _, err := s.process(t.Context(), claimed); err != nil {
		t.Fatal(err)
	}
	waitForTask(t, s, "line", statusFailed)
	select {
	case <-exec.ran:
		t.Fatal("executed an invalid action")
	default:
	}
}

func waitForTask(t *testing.T, s *Service, profile, status string) Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err := s.List(t.Context(), profile)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) == 1 && tasks[0].LastRun != nil && tasks[0].LastRun.Status == status {
			return tasks[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task for %s did not reach %s", profile, status)
	return Task{}
}

func TestClaimsSerializeAcrossSIMsAndDeletionStopsRepeat(t *testing.T) {
	db := testStore(t)
	now := time.Now()
	tasks := []storage.SIMAutomation{
		saveTestTask(t, db, "first", "line", now, 1),
		saveTestTask(t, db, "second", "other", now, 1),
	}
	var workers sync.WaitGroup
	claims := make(chan storage.SIMAutomation, len(tasks))
	for _, task := range tasks {
		workers.Go(func() {
			got, ok, err := db.ClaimSIMAutomation(t.Context(), task, task.NextAt, task.NextAt, task.NextAt+86400000)
			if err != nil {
				t.Error(err)
			}
			if ok {
				claims <- got
			}
		})
	}
	workers.Wait()
	if len(claims) != 1 {
		t.Fatalf("claims = %d, want 1", len(claims))
	}
	claimed := <-claims
	if err := db.DeleteSIMAutomation(t.Context(), claimed.ProfileID, claimed.ID); !errors.Is(err, storage.ErrSIMAutomationBusy) {
		t.Fatalf("delete claimed automation = %v", err)
	}
	if err := db.ConfirmESIMProfileDeletion(t.Context(), claimed.ProfileID, "modem", "se"); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishSIMAutomation(t.Context(), claimed, `{"status":"succeeded"}`, ""); err != nil {
		t.Fatal(err)
	}
	record, err := db.SIMAutomation(t.Context(), claimed.ProfileID, claimed.ID)
	if err != nil || record.NextAt != 0 || record.RunningAt != 0 {
		t.Fatalf("deleted profile task = %+v, %v", record, err)
	}
}

func TestAutomationHTTPContract(t *testing.T) {
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	s := &Service{store: testStore(t), executor: &fakeExecutor{}, wake: make(chan struct{}, 1), now: func() time.Time { return now }}
	server := echo.New()
	s.RegisterRoutes(server.Group("/api/v1"))
	const collection = "/api/v1/sim-profiles/line/automations"
	const scheduled = "/api/v1/modems/modem/automations"
	request := func(method, path, payload string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(payload))
		if payload != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: got %d (%s), want %d", method, path, rec.Code, rec.Body, status)
		}
		return rec
	}
	request(http.MethodPost, collection, `{"modemId":"modem","seId":"se","scheduledAt":"2026-11-21T12:00:00Z","action":{"type":"call","to":"*123#","durationSeconds":5}}`, http.StatusUnprocessableEntity)
	const create = `{"modemId":"modem","seId":"se","scheduledAt":"2026-11-21T12:00:00Z","repeatDays":2,"action":{"type":"sms","to":"+1 202 555 0123","text":" hello "}}`
	reply := request(http.MethodPost, collection, create, http.StatusCreated)
	request(http.MethodPost, collection, create, http.StatusConflict)
	var task Task
	if err := json.Unmarshal(reply.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.NextAt == nil || task.RepeatDays == nil || *task.RepeatDays != 2 || *task.Action.To != "+12025550123" || *task.Action.Text != " hello " {
		t.Fatalf("created task = %+v", task)
	}
	member := collection + "/" + task.ID
	if reply.Header().Get("Location") != member {
		t.Fatalf("Location = %q", reply.Header().Get("Location"))
	}
	summary := request(http.MethodGet, scheduled, "", http.StatusOK)
	want := fmt.Sprintf(`{"items":[{"id":%q,"profileId":"line","seId":"se","actionType":"sms","nextAt":"2026-11-21T12:00:00Z"}]}`, task.ID)
	if got := strings.TrimSpace(summary.Body.String()); got != want {
		t.Fatalf("list schedules = %s, want %s", got, want)
	}
	otherModem := request(http.MethodGet, "/api/v1/modems/other/automations", "", http.StatusOK)
	if got := strings.TrimSpace(otherModem.Body.String()); got != `{"items":[]}` {
		t.Fatalf("list schedules for another modem = %s, want no items", got)
	}
	request(http.MethodPut, member, `{"scheduledAt":"2026-11-22T12:00:00Z","action":{"type":"data","bytes":1024}}`, http.StatusOK)
	record, err := s.store.SIMAutomation(t.Context(), "line", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := s.store.ClaimSIMAutomation(t.Context(), record, record.NextAt, record.NextAt, 0)
	if err != nil || !ok {
		t.Fatalf("claim one-time task: claimed = %v, err = %v", ok, err)
	}
	if err := s.store.FinishSIMAutomation(t.Context(), claimed, `{"status":"succeeded"}`, ""); err != nil {
		t.Fatal(err)
	}
	summary = request(http.MethodGet, scheduled, "", http.StatusOK)
	want = fmt.Sprintf(`{"items":[{"id":%q,"profileId":"line","seId":"se","actionType":"data","nextAt":null}]}`, task.ID)
	if got := strings.TrimSpace(summary.Body.String()); got != want {
		t.Fatalf("list after one-time run = %s, want %s", got, want)
	}
	request(http.MethodDelete, strings.Replace(member, "/line/", "/other/", 1), "", http.StatusNotFound)
	request(http.MethodDelete, member, "", http.StatusNoContent)
	empty := request(http.MethodGet, scheduled, "", http.StatusOK)
	if got := strings.TrimSpace(empty.Body.String()); got != `{"items":[]}` {
		t.Fatalf("list schedules after delete = %s, want no items", got)
	}
	request(http.MethodGet, collection, "", http.StatusOK)
}
