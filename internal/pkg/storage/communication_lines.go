package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/damonto/sigmo/internal/pkg/storage/query"
	"time"
)

// SIMLine includes archived identities; Present is established by a fresh inventory.
type SIMLine struct {
	ProfileID     string `json:"profileId"`
	ModemID       string `json:"modemId"`
	Label         string `json:"label"`
	Number        string `json:"number"`
	Region        string `json:"region"`
	Kind          string `json:"kind"`
	SEID          string `json:"seId"`
	DeletionState string `json:"deletionState"`
	SuccessorID   string `json:"successorId"`
	MessageCount  int    `json:"messageCount"`
	CallCount     int    `json:"callCount"`
	Present       bool   `json:"present"`
	Active        bool   `json:"active"`
	CanMessage    bool   `json:"canMessage"`
	CanCall       bool   `json:"canCall"`
}

// SIMLines reads cached identities without contacting modems.
func (s *Store) SIMLines(ctx context.Context) ([]SIMLine, error) {
	rows, err := query.New(s.db).ListSIMLines(ctx)
	if err != nil {
		return nil, fmt.Errorf("list SIM lines: %w", err)
	}
	items := make([]SIMLine, 0, len(rows))
	for _, row := range rows {
		items = append(items, SIMLine{ProfileID: row.ProfileID, ModemID: row.ModemID, Label: row.Label, Number: row.Number, Region: row.Region, Kind: row.Kind, SEID: row.SEID, DeletionState: row.DeletionState, SuccessorID: row.SuccessorID, MessageCount: int(row.MessageCount), CallCount: int(row.CallCount)})
	}
	return items, nil
}

// MarkProfileDeletion persists intent before hardware changes and the confirmed result afterwards.
func (s *Store) MarkProfileDeletion(ctx context.Context, profileID, state string) error {
	err := query.New(s.db).MarkProfileDeletion(ctx, query.MarkProfileDeletionParams{State: state, ProfileID: profileID})
	if err != nil {
		return fmt.Errorf("record profile deletion: %w", err)
	}
	return nil
}

// ProfileReconciliation describes the state observed before reading hardware.
// A newer deletion or assignment must survive a delayed inventory response.
type ProfileReconciliation struct {
	ProfileID         string
	ExpectedState     string
	ExpectedSuccessor string
	NewState          string
}

// ReconcileProfileDeletion updates a state only when no operation has superseded it.
func (s *Store) ReconcileProfileDeletion(ctx context.Context, change ProfileReconciliation) (bool, error) {
	count, err := query.New(s.db).ReconcileProfileDeletion(ctx, query.ReconcileProfileDeletionParams{
		ProfileID: change.ProfileID, ExpectedState: change.ExpectedState,
		ExpectedSuccessor: change.ExpectedSuccessor, NewState: change.NewState,
	})
	if err != nil {
		return false, fmt.Errorf("reconcile profile deletion: %w", err)
	}
	return count != 0, nil
}

// CommunicationPreference belongs to a correspondent, independently of contact records.
type CommunicationPreference struct {
	Participant string `json:"participant"`
	ProfileID   string `json:"profileId"`
	UpdatedAt   string `json:"updatedAt"`
}

// RememberCommunication records only a confirmed outgoing operation.
func (s *Store) RememberCommunication(ctx context.Context, preference CommunicationPreference) error {
	preference.Participant = ParticipantKey(preference.Participant, "")
	if preference.Participant == "" || preference.ProfileID == "" {
		return nil
	}
	if preference.UpdatedAt == "" {
		preference.UpdatedAt = nowText()
	}
	err := query.New(s.db).RememberCommunication(ctx, query.RememberCommunicationParams{Participant: preference.Participant, ProfileID: preference.ProfileID, UpdatedAt: preference.UpdatedAt})
	if err != nil {
		return fmt.Errorf("remember communication line: %w", err)
	}
	return nil
}

// PreferredCommunication returns an empty selection when this correspondent has no history.
func (s *Store) PreferredCommunication(ctx context.Context, participant string) (CommunicationPreference, error) {
	result := CommunicationPreference{Participant: ParticipantKey(participant, "")}
	row, err := query.New(s.db).PreferredCommunication(ctx, result.Participant)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read preferred line: %w", err)
	}
	result.ProfileID, result.UpdatedAt = row.ProfileID, row.UpdatedAt
	return result, nil
}

// HistoryAssignment names an already-deleted source and a freshly verified target.
type HistoryAssignment struct {
	SourceProfileID string
	TargetProfileID string
}

// ErrInvalidAssignment prevents accidental movement of live or unidentified histories.
var ErrInvalidAssignment = errors.New("history assignment requires a deleted source and a different existing target")

// AssignHistory preserves carrier identities, read state, deduplication and recording keys.
func (s *Store) AssignHistory(ctx context.Context, assignment HistoryAssignment) error {
	source, target := assignment.SourceProfileID, assignment.TargetProfileID
	if source == "" || target == "" || source == target {
		return ErrInvalidAssignment
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin history assignment: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Histories and preferences move together or not at all.
	queries := query.New(tx)
	origin, err := queries.ReadAssignmentSource(ctx, source)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidAssignment
	}
	if err != nil {
		return fmt.Errorf("read assignment source: %w", err)
	}
	if origin.DeletionState == "assigned" && origin.SuccessorID == target {
		return nil
	}
	if origin.DeletionState != "deleted" {
		return ErrInvalidAssignment
	}
	valid, err := queries.ValidAssignmentTarget(ctx, target)
	if err != nil {
		return fmt.Errorf("read assignment target: %w", err)
	}
	if !valid {
		return ErrInvalidAssignment
	}
	if err := queries.AssignMessageHistory(ctx, query.AssignMessageHistoryParams{Target: target, Source: source}); err != nil {
		return fmt.Errorf("assign message history: %w", err)
	}
	if err := queries.AssignCallHistory(ctx, query.AssignCallHistoryParams{Target: target, Source: source}); err != nil {
		return fmt.Errorf("assign call history: %w", err)
	}
	if err := queries.AssignCommunicationPreferences(ctx, query.AssignCommunicationPreferencesParams{Target: target, Source: source}); err != nil {
		return fmt.Errorf("assign communication preferences: %w", err)
	}
	if err := queries.AssignProfileSuccessors(ctx, query.AssignProfileSuccessorsParams{Target: target, Source: source}); err != nil {
		return fmt.Errorf("assign profile successors: %w", err)
	}
	if err := queries.CompleteHistoryAssignment(ctx, query.CompleteHistoryAssignmentParams{Target: target, Source: source}); err != nil {
		return fmt.Errorf("complete history assignment: %w", err)
	}
	if err := advanceMessageState(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit history assignment: %w", err)
	}
	return nil
}

// CallHistory returns records independently of currently enabled SIMs.
func (s *Store) CallHistory(ctx context.Context, filter HistoryFilter) ([]Call, string, error) {
	limit, cursor, err := pageCursor(filter)
	if err != nil {
		return nil, "", err
	}
	queries := query.New(s.db)
	if cursor.ID == "" {
		cursor.Snapshot, err = queries.ReadCallSnapshot(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("read call snapshot: %w", err)
		}
	}
	var missed int64
	if filter.Missed {
		missed = 1
	}
	rows, err := queries.ListCallHistory(ctx, query.ListCallHistoryParams{ProfileID: filter.ProfileID, ModemID: filter.ModemID, Search: filter.Query, SearchPattern: likePattern(filter.Query), Missed: missed, Snapshot: cursor.Snapshot, CursorID: cursor.ID, CursorTimestamp: cursor.Timestamp, PageLimit: int64(limit + 1)})
	if err != nil {
		return nil, "", fmt.Errorf("list call history: %w", err)
	}
	items := make([]Call, 0, len(rows))
	for _, row := range rows {
		call, err := storedCall(query.ReadCallRow(row))
		if err != nil {
			return nil, "", err
		}
		items = append(items, call)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeHistoryCursor(historyCursor{Snapshot: cursor.Snapshot, Timestamp: timeText(last.StartedAt), ID: last.ID})
	}
	return items, next, nil
}

// CommunicationTime uses the confirmed operation's time instead of a delayed poll's time.
func CommunicationTime(value time.Time) string { return timeText(value) }
