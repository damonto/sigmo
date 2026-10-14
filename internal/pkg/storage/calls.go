package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// Call is a SIM-scoped call snapshot, including its exclusive media owner.
type Call struct {
	LineLabel        string
	HistoryProfileID string
	BackendID        string
	CallUUID         string
	OwnerClientID    string
	ID               string
	ProfileID        string
	ModemID          string
	Route            string
	Direction        string
	Number           string
	State            string
	Hold             string
	Reason           string
	StartedAt        time.Time
	AnsweredAt       time.Time
	EndedAt          time.Time
	UpdatedAt        time.Time
}

// EndInterruptedCalls retires snapshots from the previous server process before
// IMS starts. Carrier and RTP sessions cannot survive a process restart.
func (s *Store) EndInterruptedCalls(ctx context.Context) error {
	now := timeText(time.Now())
	queries := query.New(s.db)
	err := queries.EndInterruptedCalls(ctx, now)
	if err != nil {
		return fmt.Errorf("end interrupted calls: %w", err)
	}
	err = queries.CancelInterruptedCallInvitations(ctx)
	if err != nil {
		return fmt.Errorf("cancel interrupted call invitations: %w", err)
	}
	return nil
}

// SaveCall retains the existing answer time and client owner when an update omits them.
func (s *Store) SaveCall(ctx context.Context, call Call) error {
	call = normalizeCall(call)
	if err := validateCall(call); err != nil {
		return err
	}
	_, err := query.New(s.db).SaveCall(ctx, callParams(call))
	if err != nil {
		return fmt.Errorf("save call: %w", err)
	}
	return nil
}

// SaveCallPreservingTerminal returns the stored snapshot and whether an update was committed.
// Older events and nonterminal updates to ended calls leave the snapshot unchanged.
func (s *Store) SaveCallPreservingTerminal(ctx context.Context, call Call) (Call, bool, error) {
	call = normalizeCall(call)
	if err := validateCall(call); err != nil {
		return Call{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Call{}, false, fmt.Errorf("begin call update: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A committed update has nothing to roll back.
	queries := query.New(tx)
	row, err := queries.ReadCall(ctx, call.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Call{}, false, fmt.Errorf("read current call: %w", err)
	}
	var existing Call
	if err == nil {
		existing, err = storedCall(row)
		if err != nil {
			return Call{}, false, err
		}
	}
	// RFC3339Nano text is not chronologically sortable at mixed precisions.
	// Compare parsed times while the transaction protects the current snapshot.
	if err == nil && call.UpdatedAt.Before(existing.UpdatedAt) {
		return existing, false, nil
	}
	count, err := queries.SaveCallPreservingTerminal(ctx, query.SaveCallPreservingTerminalParams(callParams(call)))
	if err != nil {
		return Call{}, false, fmt.Errorf("save call preserving terminal state: %w", err)
	}
	if count == 0 {
		return existing, false, nil
	}
	// Read after the INSERT trigger assigns history to a previously selected successor.
	row, err = queries.ReadCall(ctx, call.ID)
	if err != nil {
		return Call{}, false, fmt.Errorf("read saved call: %w", err)
	}
	saved, err := storedCall(row)
	if err != nil {
		return Call{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Call{}, false, fmt.Errorf("commit call update: %w", err)
	}
	return saved, true, nil
}

func (s *Store) Call(ctx context.Context, id string) (Call, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Call{}, ErrNotFound
	}
	row, err := query.New(s.db).ReadCall(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Call{}, ErrNotFound
	}
	if err != nil {
		return Call{}, fmt.Errorf("read call: %w", err)
	}
	return storedCall(row)
}

func (s *Store) DeleteCall(ctx context.Context, profileID string, modemID string, id string) error {
	profileID = strings.TrimSpace(profileID)
	modemID = strings.TrimSpace(modemID)
	id = strings.TrimSpace(id)
	if profileID == "" || modemID == "" || id == "" {
		return ErrNotFound
	}
	affected, err := query.New(s.db).DeleteCall(ctx, query.DeleteCallParams{ID: id, ProfileID: profileID, ModemID: modemID})
	if err != nil {
		return fmt.Errorf("delete call: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListCalls(ctx context.Context, profileID string, modemID string, limit int, search string) ([]Call, error) {
	profileID = strings.TrimSpace(profileID)
	modemID = strings.TrimSpace(modemID)
	if profileID == "" || modemID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	terms := searchTerms(search)
	patterns := make([]string, 0, len(terms))
	for _, term := range terms {
		patterns = append(patterns, likePattern(term.value))
	}
	encodedPatterns, err := json.Marshal(patterns)
	if err != nil {
		return nil, fmt.Errorf("encode call search patterns: %w", err)
	}
	rows, err := query.New(s.db).ListCalls(ctx, query.ListCallsParams{
		ProfileID: profileID, ModemID: modemID, PageLimit: int64(limit), SearchPatterns: string(encodedPatterns),
	})
	if err != nil {
		return nil, fmt.Errorf("list calls: %w", err)
	}
	var calls []Call
	for _, row := range rows {
		call, err := storedCall(query.ReadCallRow(row))
		if err != nil {
			return nil, err
		}
		calls = append(calls, call)
	}
	return calls, nil
}

func normalizeCall(call Call) Call {
	call.ID = strings.TrimSpace(call.ID)
	call.ProfileID = strings.TrimSpace(call.ProfileID)
	call.ModemID = strings.TrimSpace(call.ModemID)
	call.Route = strings.TrimSpace(call.Route)
	call.Direction = strings.TrimSpace(call.Direction)
	call.Number = strings.TrimSpace(call.Number)
	call.State = strings.TrimSpace(call.State)
	call.Hold = strings.TrimSpace(call.Hold)
	call.Reason = strings.TrimSpace(call.Reason)
	if call.Hold == "" {
		call.Hold = "none"
	}
	if call.StartedAt.IsZero() {
		call.StartedAt = time.Now()
	}
	if call.UpdatedAt.IsZero() {
		call.UpdatedAt = call.StartedAt
	}
	return call
}

func validateCall(call Call) error {
	if call.ID == "" {
		return errors.New("call id is required")
	}
	if call.ProfileID == "" {
		return errors.New("profile id is required")
	}
	if call.ModemID == "" {
		return errors.New("modem id is required")
	}
	if call.Route == "" {
		return errors.New("call route is required")
	}
	if call.Direction == "" {
		return errors.New("call direction is required")
	}
	if call.State == "" {
		return errors.New("call state is required")
	}
	return nil
}

func callParams(call Call) query.SaveCallParams {
	return query.SaveCallParams{
		ID: call.ID, ProfileID: call.ProfileID, ModemID: call.ModemID,
		Route: call.Route, Direction: call.Direction, Number: call.Number,
		State: call.State, HoldState: call.Hold, Reason: call.Reason,
		StartedAt: timeText(call.StartedAt), AnsweredAt: timeText(call.AnsweredAt),
		EndedAt: timeText(call.EndedAt), UpdatedAt: timeText(call.UpdatedAt), OwnerClientID: call.OwnerClientID,
	}
}

func storedCall(row query.ReadCallRow) (Call, error) {
	call := Call{LineLabel: row.LineLabel, ID: row.ID, ProfileID: row.ProfileID, HistoryProfileID: row.HistoryProfileID, ModemID: row.ModemID, Route: row.Route, Direction: row.Direction, Number: row.Number, State: row.State, Hold: row.HoldState, Reason: row.Reason, OwnerClientID: row.OwnerClientID, BackendID: row.BackendID}
	if call.HistoryProfileID == "" {
		call.HistoryProfileID = call.ProfileID
	}
	var err error
	if call.StartedAt, err = parseTime(row.StartedAt); err != nil {
		return Call{}, fmt.Errorf("parse call started_at: %w", err)
	}
	if call.AnsweredAt, err = parseTime(row.AnsweredAt); err != nil {
		return Call{}, fmt.Errorf("parse call answered_at: %w", err)
	}
	if call.EndedAt, err = parseTime(row.EndedAt); err != nil {
		return Call{}, fmt.Errorf("parse call ended_at: %w", err)
	}
	if call.UpdatedAt, err = parseTime(row.UpdatedAt); err != nil {
		return Call{}, fmt.Errorf("parse call updated_at: %w", err)
	}
	call.CallUUID = CallUUID(call.BackendID, call.ModemID, call.ID)
	return call, nil
}

// CallUUID gives push invitations and snapshots the same opaque UUID across restarts.
func CallUUID(backendID, modemID, callID string) string {
	sum := sha256.Sum256([]byte(backendID + "\x00" + modemID + "\x00" + callID))
	sum[6] = sum[6]&0x0f | 0x80
	sum[8] = sum[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// ClaimCall changes only an unowned live call. The caller must separately serialize
// its carrier action; a second installation can never overwrite an existing owner.
func (s *Store) ClaimCall(ctx context.Context, callID, clientID string) (bool, error) {
	count, err := query.New(s.db).ClaimCall(ctx, query.ClaimCallParams{OwnerClientID: clientID, ID: callID})
	if err != nil {
		return false, fmt.Errorf("claim call: %w", err)
	}
	return count > 0, nil
}

// ReleaseCallClaim permits a retry after an unsuccessful answer, never after connection.
func (s *Store) ReleaseCallClaim(ctx context.Context, callID, clientID string) error {
	err := query.New(s.db).ReleaseCallClaim(ctx, query.ReleaseCallClaimParams{ID: callID, OwnerClientID: clientID})
	if err != nil {
		return fmt.Errorf("release call claim: %w", err)
	}
	return nil
}
