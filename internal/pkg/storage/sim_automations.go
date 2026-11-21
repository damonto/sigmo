package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ncruces/go-sqlite3"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// ErrSIMAutomationBusy indicates a concurrent edit, execution, or pending restoration.
var ErrSIMAutomationBusy = errors.New("automation is busy")

// ErrSIMAutomationLineUnavailable means the line is missing or has been deleted.
var ErrSIMAutomationLineUnavailable = errors.New("automation SIM line unavailable")

// ErrSIMAutomationExists means the line already has an automation.
var ErrSIMAutomationExists = errors.New("SIM line already has an automation")

// SIMAutomation is the stored schedule and claim for one SIM and modem.
// Times are Unix milliseconds, with zero representing an absent timestamp.
type SIMAutomation struct {
	ID           string
	ProfileID    string
	ModemID      string
	SEID         string
	EID          string
	ScheduledAt  int64
	RepeatDays   int64
	Action       string
	NextAt       int64
	RunningAt    int64
	StartedAt    int64
	LastRun      string
	RestoreState string
	Revision     int64
}

func automationRecord(row query.SimAutomation) SIMAutomation {
	return SIMAutomation{
		ID: row.ID, ProfileID: row.ProfileID, ModemID: row.ModemID, SEID: row.SEID, EID: row.Eid,
		ScheduledAt: row.ScheduledAt, RepeatDays: row.RepeatDays, Action: row.Action, NextAt: row.NextAt,
		RunningAt: row.RunningAt, StartedAt: row.StartedAt, LastRun: row.LastRun,
		RestoreState: row.RestoreState, Revision: row.Revision,
	}
}

// CreateSIMAutomation persists a new task.
func (s *Store) CreateSIMAutomation(ctx context.Context, task SIMAutomation) (SIMAutomation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SIMAutomation{}, fmt.Errorf("begin SIM automation creation: %w", err)
	}
	defer tx.Rollback()
	q := query.New(tx)
	line, err := q.SIMAutomationLine(ctx, task.ProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return SIMAutomation{}, ErrSIMAutomationLineUnavailable
	}
	if err != nil {
		return SIMAutomation{}, fmt.Errorf("check new automation line: %w", err)
	}
	if line.ModemID != task.ModemID || line.SEID != task.SEID || line.DeletionState != "" {
		return SIMAutomation{}, ErrSIMAutomationLineUnavailable
	}
	row, err := q.CreateSIMAutomation(ctx, query.CreateSIMAutomationParams{
		ID: task.ID, ProfileID: task.ProfileID, ModemID: task.ModemID, SEID: task.SEID, Eid: task.EID,
		ScheduledAt: task.ScheduledAt, RepeatDays: task.RepeatDays, Action: task.Action, NextAt: task.NextAt,
	})
	if errors.Is(err, sqlite3.CONSTRAINT_UNIQUE) {
		return SIMAutomation{}, ErrSIMAutomationExists
	}
	if err != nil {
		return SIMAutomation{}, fmt.Errorf("create SIM automation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return SIMAutomation{}, fmt.Errorf("commit SIM automation creation: %w", err)
	}
	return automationRecord(row), nil
}

// SIMAutomation returns a task within its owning SIM line.
func (s *Store) SIMAutomation(ctx context.Context, profileID, id string) (SIMAutomation, error) {
	row, err := query.New(s.db).ReadSIMAutomation(ctx, query.ReadSIMAutomationParams{ProfileID: profileID, ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return SIMAutomation{}, ErrNotFound
	}
	if err != nil {
		return SIMAutomation{}, fmt.Errorf("read SIM automation: %w", err)
	}
	return automationRecord(row), nil
}

// SIMAutomations lists all tasks for an ICCID, including tasks on other modems.
func (s *Store) SIMAutomations(ctx context.Context, profileID string) ([]SIMAutomation, error) {
	return automationRecords(query.New(s.db).ListSIMAutomations(ctx, profileID))
}

// SIMAutomationSummary contains the fields displayed beside a SIM line.
type SIMAutomationSummary struct {
	ID         string
	ProfileID  string
	SEID       string
	ActionType string
	NextAt     int64
}

// SIMAutomationSummaries reads the configured actions for a modem.
func (s *Store) SIMAutomationSummaries(ctx context.Context, modemID string) ([]SIMAutomationSummary, error) {
	rows, err := query.New(s.db).ListSIMAutomationSummaries(ctx, modemID)
	if err != nil {
		return nil, fmt.Errorf("list SIM automation summaries: %w", err)
	}
	items := make([]SIMAutomationSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, SIMAutomationSummary{
			ID: row.ID, ProfileID: row.ProfileID, SEID: row.SEID,
			ActionType: row.ActionType, NextAt: row.NextAt,
		})
	}
	return items, nil
}

// UpdateSIMAutomation replaces the configuration of an idle task.
func (s *Store) UpdateSIMAutomation(ctx context.Context, task SIMAutomation) (SIMAutomation, error) {
	row, err := query.New(s.db).UpdateSIMAutomation(ctx, query.UpdateSIMAutomationParams{
		ProfileID: task.ProfileID, ID: task.ID, Revision: task.Revision, ScheduledAt: task.ScheduledAt,
		RepeatDays: task.RepeatDays, Action: task.Action, NextAt: task.NextAt,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return SIMAutomation{}, ErrSIMAutomationBusy
	}
	if err != nil {
		return SIMAutomation{}, fmt.Errorf("update SIM automation: %w", err)
	}
	return automationRecord(row), nil
}

// DeleteSIMAutomation removes a task after its action and any restoration have settled.
func (s *Store) DeleteSIMAutomation(ctx context.Context, profileID, id string) error {
	n, err := query.New(s.db).DeleteSIMAutomation(ctx, query.DeleteSIMAutomationParams{ProfileID: profileID, ID: id})
	if err != nil {
		return fmt.Errorf("delete SIM automation: %w", err)
	}
	if n != 0 {
		return nil
	}
	if _, err := s.SIMAutomation(ctx, profileID, id); err != nil {
		return err
	}
	return ErrSIMAutomationBusy
}

// RecoverSIMAutomations lists interrupted claims and outstanding restorations.
func (s *Store) RecoverSIMAutomations(ctx context.Context) ([]SIMAutomation, error) {
	rows, err := query.New(s.db).PendingSIMAutomationRecovery(ctx)
	return automationRecords(rows, sqliteContextError(ctx, err))
}

// DueSIMAutomations lists due tasks in schedule order.
func (s *Store) DueSIMAutomations(ctx context.Context, now int64) ([]SIMAutomation, error) {
	rows, err := query.New(s.db).DueSIMAutomations(ctx, now)
	return automationRecords(rows, sqliteContextError(ctx, err))
}

func automationRecords(rows []query.SimAutomation, err error) ([]SIMAutomation, error) {
	if err != nil {
		return nil, fmt.Errorf("list SIM automations: %w", err)
	}
	tasks := make([]SIMAutomation, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, automationRecord(row))
	}
	return tasks, nil
}

// NextSIMAutomationAfter reports the earliest unclaimed schedule after the given Unix millisecond, or zero.
func (s *Store) NextSIMAutomationAfter(ctx context.Context, after int64) (int64, error) {
	at, err := query.New(s.db).NextSIMAutomationAfter(ctx, after)
	if err != nil {
		return 0, fmt.Errorf("read next SIM automation: %w", sqliteContextError(ctx, err))
	}
	return at, nil
}

// ClaimSIMAutomation commits the occurrence and its next schedule before any external action starts.
func (s *Store) ClaimSIMAutomation(ctx context.Context, task SIMAutomation, scheduledAt, startedAt, nextAt int64) (SIMAutomation, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SIMAutomation{}, false, fmt.Errorf("begin SIM automation claim: %w", sqliteContextError(ctx, err))
	}
	defer tx.Rollback()
	q := query.New(tx)
	busy, err := q.SIMAutomationModemBusy(ctx, task.ModemID)
	if err != nil {
		return SIMAutomation{}, false, fmt.Errorf("check SIM automation claim: %w", sqliteContextError(ctx, err))
	}
	if busy != 0 {
		return SIMAutomation{}, false, nil
	}
	row, err := q.ClaimSIMAutomation(ctx, query.ClaimSIMAutomationParams{
		ID: task.ID, Revision: task.Revision, RunningAt: scheduledAt, StartedAt: startedAt, NextAt: nextAt,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return SIMAutomation{}, false, nil
	}
	if err != nil {
		return SIMAutomation{}, false, fmt.Errorf("claim SIM automation: %w", sqliteContextError(ctx, err))
	}
	if err := tx.Commit(); err != nil {
		return SIMAutomation{}, false, fmt.Errorf("commit SIM automation claim: %w", sqliteContextError(ctx, err))
	}
	return automationRecord(row), true, nil
}

// SetSIMAutomationRestore records restoration intent before changing the active eSIM.
func (s *Store) SetSIMAutomationRestore(ctx context.Context, task SIMAutomation, state string) error {
	count, err := query.New(s.db).SetSIMAutomationRestore(ctx, query.SetSIMAutomationRestoreParams{
		ID: task.ID, Revision: task.Revision, RestoreState: state,
	})
	return automationWrite(count, sqliteContextError(ctx, err))
}

// FinishSIMAutomation records the action outcome and releases the execution claim.
func (s *Store) FinishSIMAutomation(ctx context.Context, task SIMAutomation, run, restore string) error {
	return automationWrite(query.New(s.db).FinishSIMAutomation(ctx, query.FinishSIMAutomationParams{
		ID: task.ID, Revision: task.Revision, LastRun: run, RestoreState: restore,
	}))
}

// UpdateSIMAutomationRecovery saves a subsequent restoration result.
func (s *Store) UpdateSIMAutomationRecovery(ctx context.Context, task SIMAutomation, run, restore string) error {
	return automationWrite(query.New(s.db).UpdateSIMAutomationRecovery(ctx, query.UpdateSIMAutomationRecoveryParams{
		ID: task.ID, Revision: task.Revision, LastRun: run, RestoreState: restore,
	}))
}

func automationWrite(count int64, err error) error {
	if err != nil {
		return fmt.Errorf("save SIM automation execution: %w", err)
	}
	if count == 0 {
		return ErrSIMAutomationBusy
	}
	return nil
}

// SIMAutomationLine reads the current stored location and deletion state.
func (s *Store) SIMAutomationLine(ctx context.Context, profileID string) (SIMLine, error) {
	row, err := query.New(s.db).SIMAutomationLine(ctx, profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return SIMLine{}, ErrNotFound
	}
	if err != nil {
		return SIMLine{}, fmt.Errorf("read SIM automation line: %w", err)
	}
	return SIMLine{ProfileID: row.ProfileID, ModemID: row.ModemID, Kind: row.Kind, SEID: row.SEID, DeletionState: row.DeletionState}, nil
}

func stopDeletedSIMAutomations(ctx context.Context, q *query.Queries, profileID, modemID, seID string) error {
	line, err := q.SIMAutomationLine(ctx, profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read deleted SIM line: %w", err)
	}
	if line.DeletionState != "deleted" || line.Kind != "esim" {
		return nil
	}
	if modemID != "" && (line.ModemID != modemID || line.SEID != seID) {
		return nil
	}
	err = q.StopSIMAutomationsForDeletedProfile(ctx, query.StopSIMAutomationsForDeletedProfileParams{
		ProfileID: profileID, ModemID: line.ModemID, SEID: line.SEID,
	})
	if err != nil {
		return fmt.Errorf("stop deleted SIM automations: %w", err)
	}
	return nil
}

// The SQLite driver can return SQLITE_INTERRUPT instead of ctx.Err().
func sqliteContextError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil && errors.Is(err, sqlite3.INTERRUPT) {
		return contextErr
	}
	return err
}
