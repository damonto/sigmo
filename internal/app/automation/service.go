package automation

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

var (
	// ErrLineUnavailable means the requested identity could not be confirmed.
	ErrLineUnavailable = errors.New("SIM line unavailable")
	// ErrInvalidSchedule means the task configuration is invalid.
	ErrInvalidSchedule = errors.New("invalid automation schedule")
)

type executor interface {
	bind(context.Context, storage.SIMAutomation) (string, error)
	execute(context.Context, storage.SIMAutomation, Action, func(context.Context, restoration) error) outcome
	restore(context.Context, storage.SIMAutomation, restoration) (code string, retry bool)
}

// Service manages automation tasks and their scheduler.
type Service struct {
	store    *storage.Store
	executor executor
	wake     chan struct{}
	now      func() time.Time
}

// New creates a task service using the provided modem operations.
func New(store *storage.Store, execution *Execution) *Service {
	return &Service{store: store, executor: execution, wake: make(chan struct{}, 1), now: time.Now}
}

// List reads the tasks for a SIM line without waiting for hardware.
func (s *Service) List(ctx context.Context, profileID string) ([]Task, error) {
	records, err := s.store.SIMAutomations(ctx, profileID)
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(records))
	for _, record := range records {
		task, err := taskFromRecord(record)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// ListSummaries returns the configured actions for all lines bound to a modem.
func (s *Service) ListSummaries(ctx context.Context, modemID string) ([]TaskSummary, error) {
	records, err := s.store.SIMAutomationSummaries(ctx, modemID)
	if err != nil {
		return nil, err
	}
	tasks := make([]TaskSummary, 0, len(records))
	for _, record := range records {
		switch record.ActionType {
		case actionData, actionSMS, actionCall:
			tasks = append(tasks, TaskSummary{
				ID: record.ID, ProfileID: record.ProfileID, SEID: record.SEID,
				ActionType: record.ActionType, NextAt: nextScheduleTime(record.NextAt),
			})
		}
	}
	return tasks, nil
}

// Create validates a live SIM binding and saves its first scheduled action.
func (s *Service) Create(ctx context.Context, profileID string, req CreateRequest) (Task, error) {
	record := storage.SIMAutomation{
		ID: rand.Text(), ProfileID: strings.TrimSpace(profileID),
		ModemID: strings.TrimSpace(req.ModemID), SEID: strings.TrimSpace(req.SEID),
	}
	if record.ProfileID == "" || record.ModemID == "" {
		return Task{}, fmt.Errorf("%w: select a SIM line", ErrInvalidSchedule)
	}
	if err := setSchedule(&record, req.Schedule, s.now()); err != nil {
		return Task{}, err
	}
	eid, err := s.executor.bind(ctx, record)
	if err != nil {
		return Task{}, err
	}
	if record.NextAt <= s.now().UnixMilli() {
		return Task{}, fmt.Errorf("%w: schedule time must be in the future", ErrInvalidSchedule)
	}
	record.EID = eid
	record, err = s.store.CreateSIMAutomation(ctx, record)
	if err != nil {
		return Task{}, err
	}
	s.signal()
	return taskFromRecord(record)
}

// Update replaces the action and schedule while keeping the original binding.
func (s *Service) Update(ctx context.Context, profileID, id string, schedule Schedule) (Task, error) {
	record, err := s.store.SIMAutomation(ctx, profileID, id)
	if err != nil {
		return Task{}, err
	}
	line, err := s.store.SIMAutomationLine(ctx, profileID)
	if err != nil {
		return Task{}, err
	}
	if line.ModemID == record.ModemID && line.SEID == record.SEID && line.DeletionState != "" {
		return Task{}, ErrLineUnavailable
	}
	if err := setSchedule(&record, schedule, s.now()); err != nil {
		return Task{}, err
	}
	record, err = s.store.UpdateSIMAutomation(ctx, record)
	if err != nil {
		return Task{}, err
	}
	s.signal()
	return taskFromRecord(record)
}

// Delete removes an idle task, including its last execution result.
func (s *Service) Delete(ctx context.Context, profileID, id string) error {
	if err := s.store.DeleteSIMAutomation(ctx, profileID, id); err != nil {
		return err
	}
	s.signal()
	return nil
}

func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func setSchedule(record *storage.SIMAutomation, schedule Schedule, now time.Time) error {
	if err := schedule.validate(now); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSchedule, err)
	}
	action, err := json.Marshal(schedule.Action)
	if err != nil {
		return fmt.Errorf("encode automation action: %w", err)
	}
	record.ScheduledAt = schedule.ScheduledAt.UnixMilli()
	record.NextAt = record.ScheduledAt
	record.RepeatDays = 0
	if schedule.RepeatDays != nil {
		record.RepeatDays = int64(*schedule.RepeatDays)
	}
	record.Action = string(action)
	return nil
}

func taskFromRecord(record storage.SIMAutomation) (Task, error) {
	task := Task{
		ID: record.ID, ProfileID: record.ProfileID, ModemID: record.ModemID, SEID: record.SEID,
		NextAt:  nextScheduleTime(record.NextAt),
		Running: record.RunningAt != 0, Restoring: record.RestoreState != "",
		Schedule: Schedule{ScheduledAt: scheduleTime(record.ScheduledAt)},
	}
	if record.RepeatDays != 0 {
		days := int(record.RepeatDays)
		task.RepeatDays = &days
	}
	if err := json.Unmarshal([]byte(record.Action), &task.Action); err != nil {
		return Task{}, fmt.Errorf("decode automation action: %w", err)
	}
	if record.LastRun != "" {
		task.LastRun = new(Run)
		if err := json.Unmarshal([]byte(record.LastRun), task.LastRun); err != nil {
			return Task{}, fmt.Errorf("decode automation result: %w", err)
		}
	}
	return task, nil
}

func scheduleTime(millis int64) time.Time {
	return time.UnixMilli(millis).UTC()
}

func nextScheduleTime(millis int64) *time.Time {
	if millis == 0 {
		return nil
	}
	next := scheduleTime(millis)
	return &next
}
