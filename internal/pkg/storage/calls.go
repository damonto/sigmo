package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Call struct {
	ID         string
	ProfileID  string
	ModemID    string
	Route      string
	Direction  string
	Number     string
	State      string
	Reason     string
	StartedAt  time.Time
	AnsweredAt time.Time
	EndedAt    time.Time
	UpdatedAt  time.Time
}

func (s *Store) SaveCall(ctx context.Context, call Call) error {
	call = normalizeCall(call)
	if err := validateCall(call); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO calls (
			id, profile_id, modem_id, route, direction, number, state, reason,
			started_at, answered_at, ended_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			profile_id = excluded.profile_id,
			modem_id = excluded.modem_id,
			route = excluded.route,
			direction = excluded.direction,
			number = excluded.number,
			state = excluded.state,
			reason = excluded.reason,
			started_at = excluded.started_at,
			answered_at = excluded.answered_at,
			ended_at = excluded.ended_at,
			updated_at = excluded.updated_at
	`, call.ID, call.ProfileID, call.ModemID, call.Route, call.Direction, call.Number, call.State, call.Reason,
		timeText(call.StartedAt), timeText(call.AnsweredAt), timeText(call.EndedAt), timeText(call.UpdatedAt))
	if err != nil {
		return fmt.Errorf("save call: %w", err)
	}
	return nil
}

func (s *Store) GetCall(ctx context.Context, id string) (Call, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Call{}, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, profile_id, modem_id, route, direction, number, state, reason,
			started_at, answered_at, ended_at, updated_at
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

func (s *Store) ListCalls(ctx context.Context, profileID string, modemID string, limit int) ([]Call, error) {
	profileID = strings.TrimSpace(profileID)
	modemID = strings.TrimSpace(modemID)
	if profileID == "" || modemID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, profile_id, modem_id, route, direction, number, state, reason,
			started_at, answered_at, ended_at, updated_at
		FROM calls
		WHERE profile_id = ? AND modem_id = ?
		ORDER BY updated_at DESC
		LIMIT ?
	`, profileID, modemID, limit)
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
	call.Reason = strings.TrimSpace(call.Reason)
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
	if err := row.Scan(
		&call.ID,
		&call.ProfileID,
		&call.ModemID,
		&call.Route,
		&call.Direction,
		&call.Number,
		&call.State,
		&call.Reason,
		&startedAt,
		&answeredAt,
		&endedAt,
		&updatedAt,
	); err != nil {
		return Call{}, fmt.Errorf("scan call: %w", err)
	}
	call.StartedAt = parseTime(startedAt)
	call.AnsweredAt = parseTime(answeredAt)
	call.EndedAt = parseTime(endedAt)
	call.UpdatedAt = parseTime(updatedAt)
	return call, nil
}
