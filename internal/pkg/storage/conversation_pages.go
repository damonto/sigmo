package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// ConversationMessagePage contains bounded history and read state from one snapshot.
type ConversationMessagePage struct {
	MessageState
	Conversation Conversation `json:"conversation"`
	Items        []Message    `json:"-"`
	NextCursor   string       `json:"nextCursor"`
}

// ConversationMessagesPage pages original SIM history, including after reassignment.
func (s *Store) ConversationMessagesPage(ctx context.Context, id string, filter HistoryFilter) (ConversationMessagePage, error) {
	limit, cursor, err := pageCursor(filter)
	if err != nil {
		return ConversationMessagePage{}, err
	}
	var cursorID int64
	if cursor.ID != "" {
		cursorID, err = strconv.ParseInt(cursor.ID, 10, 64)
		if err != nil || cursorID <= 0 {
			return ConversationMessagePage{}, ErrInvalidHistoryCursor
		}
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ConversationMessagePage{}, fmt.Errorf("begin conversation page: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Read state and messages share this snapshot.
	queries := query.New(tx)
	row, err := queries.ReadConversation(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ConversationMessagePage{}, ErrNotFound
	}
	if err != nil {
		return ConversationMessagePage{}, fmt.Errorf("read conversation metadata: %w", err)
	}
	state, err := readMessageState(ctx, tx)
	if err != nil {
		return ConversationMessagePage{}, err
	}
	var rows []query.ListConversationMessagePageRow
	if cursor.ID == "" {
		cursor.Snapshot, err = queries.ReadMessageSnapshot(ctx)
		if err != nil {
			return ConversationMessagePage{}, fmt.Errorf("read conversation snapshot: %w", err)
		}
		rows, err = queries.ListConversationMessagePage(ctx, query.ListConversationMessagePageParams{
			ConversationID: id, Snapshot: cursor.Snapshot, PageLimit: int64(limit + 1),
		})
	} else {
		rows, err = conversationMessagesAfterCursor(ctx, queries, id, cursor, cursorID, limit+1)
	}
	if err != nil {
		return ConversationMessagePage{}, fmt.Errorf("read conversation page: %w", err)
	}
	result := ConversationMessagePage{MessageState: state, Conversation: conversationFromRow(row), Items: []Message{}}
	for _, row := range rows {
		stamp, err := parseTime(row.Timestamp)
		if err != nil {
			return ConversationMessagePage{}, fmt.Errorf("parse conversation page timestamp: %w", err)
		}
		result.Items = append(result.Items, Message{
			ID: row.ID, ConversationID: row.ConversationID, ProfileID: row.ProfileID, ModemID: row.ModemID,
			Sender: row.Sender, Recipient: row.Recipient, Text: row.Text, Timestamp: stamp,
			Status: row.Status, Incoming: row.Incoming != 0, Routed: row.WifiCalling != 0,
		})
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		last := rows[limit-1]
		result.NextCursor = encodeHistoryCursor(historyCursor{Snapshot: cursor.Snapshot, Timestamp: last.Timestamp, ID: strconv.FormatInt(last.ID, 10)})
	}
	slices.Reverse(result.Items)
	return result, nil
}

func conversationMessagesAfterCursor(ctx context.Context, queries *query.Queries, id string, cursor historyCursor, cursorID int64, limit int) ([]query.ListConversationMessagePageRow, error) {
	// SQLite cannot seek a row-value comparison on this expression index. Split
	// the timestamp/ID boundary into disjoint ranges so ties also use an ID seek.
	sameTime, err := queries.ListConversationMessagesAtTime(ctx, query.ListConversationMessagesAtTimeParams{
		ConversationID: id, Snapshot: cursor.Snapshot, CursorTimestamp: cursor.Timestamp,
		CursorID: cursorID, PageLimit: int64(limit),
	})
	if err != nil {
		return nil, err
	}
	rows := make([]query.ListConversationMessagePageRow, 0, limit)
	for _, row := range sameTime {
		rows = append(rows, query.ListConversationMessagePageRow(row))
	}
	if len(rows) == limit {
		return rows, nil
	}
	older, err := queries.ListConversationMessagesBeforeTime(ctx, query.ListConversationMessagesBeforeTimeParams{
		ConversationID: id, Snapshot: cursor.Snapshot, CursorTimestamp: cursor.Timestamp,
		PageLimit: int64(limit - len(rows)),
	})
	if err != nil {
		return nil, err
	}
	for _, row := range older {
		rows = append(rows, query.ListConversationMessagePageRow(row))
	}
	return rows, nil
}
