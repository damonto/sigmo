package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	maxConcurrentModems = 8
	recoveryInterval    = 30 * time.Second
	shutdownCleanup     = 15 * time.Second
)

type workerResult struct {
	modemID string
	retry   bool
	err     error
}

// Run dispatches due work and recovers interrupted tasks. It joins its workers
// before returning, including when cancellation interrupts the scheduler.
func (s *Service) Run(ctx context.Context) (runErr error) {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	// Each active modem sends exactly one result. The buffer lets workers finish
	// and report persistence errors even after the scheduler stops receiving.
	results := make(chan workerResult, maxConcurrentModems)
	defer func() {
		cancel()
		workers.Wait()
		close(results)
		for result := range results {
			runErr = errors.Join(runErr, result.err)
		}
	}()
	active := make(map[string]bool)
	retryAfter := make(map[string]time.Time)
	start := func(task storage.SIMAutomation, run func(context.Context, storage.SIMAutomation) (bool, error)) {
		active[task.ModemID] = true
		workers.Go(func() {
			retry, err := run(ctx, task)
			results <- workerResult{modemID: task.ModemID, retry: retry, err: err}
		})
	}

	for ctx.Err() == nil {
		now := s.now()
		recovery, err := s.store.RecoverSIMAutomations(ctx)
		if err != nil {
			return err
		}
		for _, task := range recovery {
			if len(active) >= maxConcurrentModems {
				break
			}
			if !active[task.ModemID] && !now.Before(retryAfter[task.ModemID]) {
				start(task, s.recover)
			}
		}

		due, err := s.store.DueSIMAutomations(ctx, now.UnixMilli())
		if err != nil {
			return err
		}
		for _, task := range due {
			if len(active) >= maxConcurrentModems {
				break
			}
			if active[task.ModemID] || now.Before(retryAfter[task.ModemID]) {
				continue
			}
			scheduled, next := occurrence(task, now.UnixMilli())
			claimed, ok, err := s.store.ClaimSIMAutomation(ctx, task, scheduled, now.UnixMilli(), next)
			if err != nil {
				return err
			}
			if ok {
				start(claimed, s.process)
			}
		}

		// An overdue task can be waiting for a busy modem. Keep its deadline
		// from hiding future occurrences on other modems.
		next, err := s.store.NextSIMAutomationAfter(ctx, now.UnixMilli())
		if err != nil {
			return err
		}
		delay := recoveryInterval
		if next != 0 {
			delay = min(delay, scheduleTime(next).Sub(s.now()))
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-s.wake:
		case <-timer.C:
		case result := <-results:
			delete(active, result.modemID)
			if result.err != nil {
				timer.Stop()
				return result.err
			}
			if result.retry {
				retryAfter[result.modemID] = s.now().Add(recoveryInterval)
			} else {
				delete(retryAfter, result.modemID)
			}
		}
		timer.Stop()
	}
	return ctx.Err()
}

func occurrence(task storage.SIMAutomation, now int64) (scheduled, next int64) {
	if task.RepeatDays == 0 {
		return task.NextAt, 0
	}
	interval := task.RepeatDays * (24 * time.Hour).Milliseconds()
	scheduled = task.NextAt + ((now-task.NextAt)/interval)*interval
	return scheduled, scheduled + interval
}

func (s *Service) process(ctx context.Context, task storage.SIMAutomation) (bool, error) {
	var action Action
	if err := json.Unmarshal([]byte(task.Action), &action); err != nil {
		return false, s.recordInvalidAction(ctx, task)
	}
	if err := action.validate(); err != nil {
		return false, s.recordInvalidAction(ctx, task)
	}

	var state string
	var journalErr error
	result := s.executor.execute(ctx, task, action, func(ctx context.Context, intent restoration) error {
		value, err := json.Marshal(intent)
		if err != nil {
			return fmt.Errorf("encode SIM automation restoration: %w", err)
		}
		journalErr = s.store.SetSIMAutomationRestore(ctx, task, string(value))
		if journalErr == nil {
			state = string(value)
		}
		return journalErr
	})
	if ctx.Err() != nil && result.Status == statusFailed {
		result.Status = statusInterrupted
	}
	run := s.newRun(task, result)
	if state != "" {
		state, run.RestoreCode = s.restore(ctx, task, state)
	}
	if err := s.saveRun(ctx, task, run, state, true); err != nil {
		return false, err
	}
	slog.InfoContext(ctx, "SIM automation finished", "task_id", task.ID, "status", run.Status, "code", run.Code, "restore_code", run.RestoreCode)
	// A canceled journal write prevented the switch. The task result is already saved.
	if errors.Is(journalErr, context.Canceled) || errors.Is(journalErr, context.DeadlineExceeded) {
		journalErr = nil
	}
	return state != "", journalErr
}

func (s *Service) recordInvalidAction(ctx context.Context, task storage.SIMAutomation) error {
	slog.ErrorContext(ctx, "invalid stored SIM automation action", "task_id", task.ID)
	return s.saveRun(ctx, task, s.newRun(task, failed("")), "", true)
}

func (s *Service) recover(ctx context.Context, task storage.SIMAutomation) (bool, error) {
	interrupted := task.RunningAt != 0
	var run Run
	if interrupted {
		run = s.newRun(task, outcome{Status: statusInterrupted})
	} else if err := json.Unmarshal([]byte(task.LastRun), &run); err != nil {
		return false, fmt.Errorf("decode automation to restore: %w", err)
	}
	state := task.RestoreState
	if state != "" {
		state, run.RestoreCode = s.restore(ctx, task, state)
	}
	return state != "", s.saveRun(ctx, task, run, state, interrupted)
}

func (s *Service) restore(ctx context.Context, task storage.SIMAutomation, state string) (string, string) {
	var intent restoration
	if err := json.Unmarshal([]byte(state), &intent); err != nil || intent.Previous == "" || intent.Slot == 0 {
		slog.ErrorContext(ctx, "invalid stored SIM automation restoration", "task_id", task.ID)
		return "", "restore_failed"
	}
	var code string
	retry := true
	if ctx.Err() == nil {
		code, retry = s.executor.restore(ctx, task, intent)
	}
	if retry && ctx.Err() != nil {
		// Shutdown may interrupt the first attempt after a SIM switch has begun.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownCleanup)
		defer cancel()
		code, retry = s.executor.restore(cleanupCtx, task, intent)
	}
	if retry {
		return state, code
	}
	return "", code
}

func (s *Service) newRun(task storage.SIMAutomation, result outcome) Run {
	return Run{
		ScheduledAt: scheduleTime(task.RunningAt), StartedAt: scheduleTime(task.StartedAt),
		FinishedAt: s.now().UTC(), Status: result.Status, Code: result.Code, BytesReceived: result.BytesReceived,
	}
}

func (s *Service) saveRun(ctx context.Context, task storage.SIMAutomation, run Run, state string, claimed bool) error {
	value, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("encode automation result: %w", err)
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if claimed {
		return s.store.FinishSIMAutomation(saveCtx, task, string(value), state)
	}
	return s.store.UpdateSIMAutomationRecovery(saveCtx, task, string(value), state)
}
