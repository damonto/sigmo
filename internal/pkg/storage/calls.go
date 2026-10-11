package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Call is a SIM-scoped call snapshot, including its exclusive media owner.
type Call struct {
	BackendID     string
	CallUUID      string
	OwnerClientID string
	ID            string
	ProfileID     string
	ModemID       string
	Route         string
	Direction     string
	Number        string
	State         string
	Hold          string
	Reason        string
	StartedAt     time.Time
	AnsweredAt    time.Time
	EndedAt       time.Time
	UpdatedAt     time.Time
}

const callColumns = `id, profile_id, modem_id, route, direction, number, state, hold_state, reason,
	started_at, answered_at, ended_at, updated_at, owner_client_id,
	(SELECT backend_id FROM message_state WHERE id = 1)`

// EndInterruptedCalls retires snapshots from the previous server process before
// IMS starts. Carrier and RTP sessions cannot survive a process restart.
func (s *Store) EndInterruptedCalls(ctx context.Context) error {
	now := timeText(time.Now())
	_, err := s.db.ExecContext(ctx, `UPDATE calls SET state = 'ended', reason = 'server_restarted', ended_at = ?, updated_at = ? WHERE state NOT IN ('ended', 'failed')`, now, now)
	if err != nil {
		return fmt.Errorf("end interrupted calls: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM native_call_outbox WHERE delivered = 0`)
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
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO calls (
			id, profile_id, modem_id, route, direction, number, state, hold_state, reason,
			started_at, answered_at, ended_at, updated_at, owner_client_id
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			profile_id = excluded.profile_id,
			modem_id = excluded.modem_id,
			route = excluded.route,
			direction = excluded.direction,
			number = excluded.number,
			state = excluded.state,
			hold_state = excluded.hold_state,
			reason = excluded.reason,
			started_at = excluded.started_at,
			answered_at = CASE
				WHEN excluded.answered_at = ? THEN calls.answered_at
				ELSE excluded.answered_at
			END,
			ended_at = excluded.ended_at,
			updated_at = excluded.updated_at,
			owner_client_id = CASE WHEN excluded.owner_client_id <> '' THEN excluded.owner_client_id ELSE calls.owner_client_id END
	`, call.ID, call.ProfileID, call.ModemID, call.Route, call.Direction, call.Number, call.State, call.Hold, call.Reason,
		timeText(call.StartedAt), timeText(call.AnsweredAt), timeText(call.EndedAt), timeText(call.UpdatedAt), call.OwnerClientID, timeText(time.Time{}))
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
	existing, err := scanCall(tx.QueryRowContext(ctx,
		`SELECT `+callColumns+` FROM calls WHERE id = ?`, call.ID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Call{}, false, fmt.Errorf("read current call: %w", err)
	}
	// RFC3339Nano text is not chronologically sortable at mixed precisions.
	// Compare parsed times while the transaction protects the current snapshot.
	if err == nil && call.UpdatedAt.Before(existing.UpdatedAt) {
		return existing, false, nil
	}
	row := tx.QueryRowContext(ctx, `
		INSERT INTO calls (
			id, profile_id, modem_id, route, direction, number, state, hold_state, reason,
			started_at, answered_at, ended_at, updated_at, owner_client_id
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			profile_id = excluded.profile_id,
			modem_id = excluded.modem_id,
			route = excluded.route,
			direction = excluded.direction,
			number = excluded.number,
			state = excluded.state,
			hold_state = excluded.hold_state,
			reason = excluded.reason,
			started_at = excluded.started_at,
			answered_at = CASE
				WHEN excluded.answered_at = ? THEN calls.answered_at
				ELSE excluded.answered_at
			END,
			ended_at = excluded.ended_at,
			updated_at = excluded.updated_at,
			owner_client_id = CASE WHEN excluded.owner_client_id <> '' THEN excluded.owner_client_id ELSE calls.owner_client_id END
		WHERE NOT (calls.state IN ('ended', 'failed') AND excluded.state NOT IN ('ended', 'failed'))
		RETURNING `+callColumns+`
	`, call.ID, call.ProfileID, call.ModemID, call.Route, call.Direction, call.Number, call.State, call.Hold, call.Reason,
		timeText(call.StartedAt), timeText(call.AnsweredAt), timeText(call.EndedAt), timeText(call.UpdatedAt), call.OwnerClientID, timeText(time.Time{}))
	saved, err := scanCall(row)
	if errors.Is(err, sql.ErrNoRows) {
		return existing, false, nil
	}
	if err != nil {
		return Call{}, false, fmt.Errorf("save call preserving terminal state: %w", err)
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
	row := s.db.QueryRowContext(ctx, `
		SELECT `+callColumns+`
		FROM calls
		WHERE id = ?
	`, id)
	call, err := scanCall(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Call{}, ErrNotFound
	}
	if err != nil {
		return Call{}, err
	}
	return call, nil
}

func (s *Store) DeleteCall(ctx context.Context, profileID string, modemID string, id string) error {
	profileID = strings.TrimSpace(profileID)
	modemID = strings.TrimSpace(modemID)
	id = strings.TrimSpace(id)
	if profileID == "" || modemID == "" || id == "" {
		return ErrNotFound
	}
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM calls
		WHERE id = ? AND profile_id = ? AND modem_id = ?
	`, id, profileID, modemID)
	if err != nil {
		return fmt.Errorf("delete call: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete call: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListCalls(ctx context.Context, profileID string, modemID string, limit int, query string) ([]Call, error) {
	profileID = strings.TrimSpace(profileID)
	modemID = strings.TrimSpace(modemID)
	if profileID == "" || modemID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	terms := searchTerms(query)
	args := []any{profileID, modemID}
	searchSQL := ""
	if len(terms) > 0 {
		clauses := make([]string, 0, len(terms))
		for _, term := range terms {
			clauses = append(clauses, `number LIKE ? ESCAPE '\'`)
			args = append(args, likePattern(term.value))
		}
		searchSQL = " AND (" + strings.Join(clauses, " OR ") + ")"
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+callColumns+`
		FROM calls
		WHERE profile_id = ? AND modem_id = ?`+searchSQL+`
		ORDER BY updated_at DESC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list calls: %w", err)
	}
	defer rows.Close()

	var calls []Call
	for rows.Next() {
		call, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		calls = append(calls, call)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list calls: %w", err)
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

type callScanner interface {
	Scan(dest ...any) error
}

func scanCall(row callScanner) (Call, error) {
	var call Call
	var startedAt, answeredAt, endedAt, updatedAt string
	var err error
	if err := row.Scan(
		&call.ID,
		&call.ProfileID,
		&call.ModemID,
		&call.Route,
		&call.Direction,
		&call.Number,
		&call.State,
		&call.Hold,
		&call.Reason,
		&startedAt,
		&answeredAt,
		&endedAt,
		&updatedAt,
		&call.OwnerClientID,
		&call.BackendID,
	); err != nil {
		return Call{}, fmt.Errorf("scan call: %w", err)
	}
	if call.StartedAt, err = parseTime(startedAt); err != nil {
		return Call{}, fmt.Errorf("parse call started_at: %w", err)
	}
	if call.AnsweredAt, err = parseTime(answeredAt); err != nil {
		return Call{}, fmt.Errorf("parse call answered_at: %w", err)
	}
	if call.EndedAt, err = parseTime(endedAt); err != nil {
		return Call{}, fmt.Errorf("parse call ended_at: %w", err)
	}
	if call.UpdatedAt, err = parseTime(updatedAt); err != nil {
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
	result, err := s.db.ExecContext(ctx, `UPDATE calls SET owner_client_id = ? WHERE id = ? AND owner_client_id = '' AND state NOT IN ('ended', 'failed')`, clientID, callID)
	if err != nil {
		return false, fmt.Errorf("claim call: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count claimed calls: %w", err)
	}
	return count > 0, nil
}

// ReleaseCallClaim permits a retry after an unsuccessful answer, never after connection.
func (s *Store) ReleaseCallClaim(ctx context.Context, callID, clientID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE calls SET owner_client_id = '' WHERE id = ? AND owner_client_id = ? AND state IN ('ringing', 'answering')`, callID, clientID)
	if err != nil {
		return fmt.Errorf("release call claim: %w", err)
	}
	return nil
}
