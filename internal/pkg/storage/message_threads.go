package storage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/damonto/sigmo/internal/pkg/storage/query"
	"slices"
	"strconv"
	"strings"
)

// HistoryFilter scopes a paged archive without requiring live hardware.
type HistoryFilter struct {
	ModemID   string
	ProfileID string
	Query     string
	Cursor    string
	Limit     int
	Missed    bool
}

// Thread returns the correspondent identity even when a line filter hides all messages.
func (s *Store) Thread(ctx context.Context, id string) (MessageThread, error) {
	participant, err := threadParticipant(id)
	if err != nil {
		return MessageThread{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return MessageThread{}, fmt.Errorf("begin thread detail: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Keep counts and preview in one read snapshot.
	thread := MessageThread{ID: id, Participant: participant}
	queries := query.New(tx)
	row, err := queries.ReadThreadLatest(ctx, participant)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageThread{}, ErrNotFound
	}
	if err != nil {
		return thread, fmt.Errorf("read latest thread message: %w", err)
	}
	thread.LatestMessage = threadMessage(query.ListThreadMessagesRow(row))
	counts, err := queries.ReadThreadCounts(ctx, participant)
	if err != nil {
		return thread, fmt.Errorf("read thread counts: %w", err)
	}
	thread.UnreadCount, thread.MarkedUnread = int(counts.UnreadCount), counts.MarkedUnread != 0
	return thread, nil
}

// ThreadMessage keeps immutable source identity separate from current history ownership.
type ThreadMessage struct {
	ID               int64  `json:"id"`
	ConversationID   string `json:"conversationId"`
	ProfileID        string `json:"profileId"`
	HistoryProfileID string `json:"historyProfileId"`
	ModemID          string `json:"modemId"`
	Sender           string `json:"sender"`
	Recipient        string `json:"recipient"`
	Text             string `json:"text"`
	Timestamp        string `json:"timestamp"`
	Incoming         bool   `json:"incoming"`
	Read             bool   `json:"read"`
	Status           string `json:"status"`
	LineLabel        string `json:"lineLabel"`
	LineNumber       string `json:"lineNumber"`
}

// MessageThread groups one correspondent across this backend's SIMs.
type MessageThread struct {
	ID            string        `json:"id"`
	Participant   string        `json:"participant"`
	LatestMessage ThreadMessage `json:"latestMessage"`
	UnreadCount   int           `json:"unreadCount"`
	MarkedUnread  bool          `json:"markedUnread"`
}

// ThreadPage includes the authoritative backend badge independently of list filters.
type ThreadPage struct {
	MessageState
	Items      []MessageThread `json:"items"`
	NextCursor string          `json:"nextCursor"`
}

// ThreadMessagesPage orders displayed history oldest first within each page.
type ThreadMessagesPage struct {
	Items      []ThreadMessage `json:"items"`
	NextCursor string          `json:"nextCursor"`
}

// ThreadRead acknowledges explicit displayed IDs, never a maximum across SIMs.
type ThreadRead struct {
	MessageIDs   []int64 `json:"messageIds"`
	MarkedUnread *bool   `json:"markedUnread,omitempty"`
}

// ThreadID is independent of a contact's name, avatar and selected SIM.
func ThreadID(participant string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(participant))
}

func threadParticipant(id string) (string, error) {
	value, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(value) == 0 || len(value) > 512 {
		return "", ErrNotFound
	}
	return string(value), nil
}

// historyCursor pins the insertion snapshot and uses the last displayed sort key.
// New arrivals cannot shift subsequent pages; deletions cannot cause skipped rows.
type historyCursor struct {
	Snapshot  int64  `json:"s"`
	Timestamp string `json:"t"`
	ID        string `json:"i"`
}

// ErrInvalidHistoryCursor rejects malformed pagination state rather than silently restarting.
var ErrInvalidHistoryCursor = errors.New("invalid history cursor")

func pageCursor(filter HistoryFilter) (int, historyCursor, error) {
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var cursor historyCursor
	if filter.Cursor == "" {
		return limit, cursor, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
	if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Snapshot < 0 || cursor.ID == "" {
		return 0, cursor, ErrInvalidHistoryCursor
	}
	if _, err := parseTime(cursor.Timestamp); err != nil {
		return 0, cursor, ErrInvalidHistoryCursor
	}
	return limit, cursor, nil
}

func encodeHistoryCursor(cursor historyCursor) string {
	// These concrete fields are always JSON encodable.
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

// MessageThreads leaves source conversations and their notification identities intact.
func (s *Store) MessageThreads(ctx context.Context, filter HistoryFilter) (ThreadPage, error) {
	limit, cursor, err := pageCursor(filter)
	if err != nil {
		return ThreadPage{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ThreadPage{}, fmt.Errorf("begin thread snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A read transaction needs no commit.
	state, err := readMessageState(ctx, tx)
	if err != nil {
		return ThreadPage{}, err
	}
	queries := query.New(tx)
	if cursor.ID == "" {
		cursor.Snapshot, err = queries.ReadMessageSnapshot(ctx)
		if err != nil {
			return ThreadPage{}, fmt.Errorf("read thread snapshot: %w", err)
		}
	}
	rows, err := queries.ListMessageThreads(ctx, query.ListMessageThreadsParams{ProfileID: filter.ProfileID, ModemID: filter.ModemID, Search: filter.Query, SearchPattern: likePattern(filter.Query), Snapshot: cursor.Snapshot, CursorID: cursor.ID, CursorTimestamp: cursor.Timestamp, PageLimit: int64(limit + 1)})
	if err != nil {
		return ThreadPage{}, fmt.Errorf("list message threads: %w", err)
	}
	result := ThreadPage{MessageState: state, Items: []MessageThread{}}
	for _, row := range rows {
		result.Items = append(result.Items, MessageThread{ID: ThreadID(row.ParticipantKey), Participant: row.ParticipantKey, UnreadCount: int(row.UnreadCount), MarkedUnread: row.MarkedUnread != 0})
	}
	more := len(result.Items) > limit
	if more {
		result.Items = result.Items[:limit]
	}
	for i := range result.Items {
		row, err := queries.ReadThreadMessage(ctx, rows[i].ID)
		if err != nil {
			return result, fmt.Errorf("read latest thread message: %w", err)
		}
		result.Items[i].LatestMessage = threadMessage(query.ListThreadMessagesRow(row))
	}
	if more {
		last := result.Items[len(result.Items)-1].LatestMessage
		result.NextCursor = encodeHistoryCursor(historyCursor{Snapshot: cursor.Snapshot, Timestamp: last.Timestamp, ID: strconv.FormatInt(last.ID, 10)})
	}
	return result, nil
}

func threadMessage(row query.ListThreadMessagesRow) ThreadMessage {
	return ThreadMessage{ID: row.ID, ConversationID: row.ConversationID, ProfileID: row.ProfileID, HistoryProfileID: row.HistoryProfileID, ModemID: row.ModemID, Sender: row.Sender, Recipient: row.Recipient, Text: row.Text, Timestamp: row.Timestamp, Incoming: row.Incoming != 0, Read: row.IsRead != 0, Status: row.Status, LineLabel: row.LineLabel, LineNumber: row.LineNumber}
}

// ThreadMessages reads an archive even after the original modem has disappeared.
func (s *Store) ThreadMessages(ctx context.Context, id string, filter HistoryFilter) (ThreadMessagesPage, error) {
	participant, err := threadParticipant(id)
	if err != nil {
		return ThreadMessagesPage{}, err
	}
	limit, cursor, err := pageCursor(filter)
	if err != nil {
		return ThreadMessagesPage{}, err
	}
	queries := query.New(s.db)
	if cursor.ID == "" {
		cursor.Snapshot, err = queries.ReadMessageSnapshot(ctx)
		if err != nil {
			return ThreadMessagesPage{}, fmt.Errorf("read message snapshot: %w", err)
		}
	}
	rows, err := queries.ListThreadMessages(ctx, query.ListThreadMessagesParams{Participant: participant, ProfileID: filter.ProfileID, ModemID: filter.ModemID, Snapshot: cursor.Snapshot, CursorID: cursor.ID, CursorTimestamp: cursor.Timestamp, PageLimit: int64(limit + 1)})
	if err != nil {
		return ThreadMessagesPage{}, fmt.Errorf("read message thread: %w", err)
	}
	result := ThreadMessagesPage{Items: make([]ThreadMessage, 0, len(rows))}
	for _, row := range rows {
		result.Items = append(result.Items, threadMessage(row))
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		last := result.Items[len(result.Items)-1]
		result.NextCursor = encodeHistoryCursor(historyCursor{Snapshot: cursor.Snapshot, Timestamp: last.Timestamp, ID: strconv.FormatInt(last.ID, 10)})
	}
	slices.Reverse(result.Items)
	return result, nil
}

// ReadThread updates only messages belonging to this correspondent.
func (s *Store) ReadThread(ctx context.Context, id string, read ThreadRead) error {
	participant, err := threadParticipant(id)
	if err != nil {
		return err
	}
	if len(read.MessageIDs) > 1000 || (len(read.MessageIDs) == 0 && read.MarkedUnread == nil) {
		return ErrInvalidReadUpdate
	}
	ids, err := json.Marshal(read.MessageIDs)
	if err != nil {
		return fmt.Errorf("encode displayed messages: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin thread read: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A committed read has nothing to roll back.
	queries := query.New(tx)
	invalid, err := queries.CountInvalidThreadReads(ctx, query.CountInvalidThreadReadsParams{MessageJSON: string(ids), Participant: participant})
	if err != nil {
		return fmt.Errorf("validate displayed messages: %w", err)
	}
	if invalid != 0 {
		return ErrInvalidReadUpdate
	}
	if err := queries.ReadDisplayedMessages(ctx, string(ids)); err != nil {
		return fmt.Errorf("mark displayed messages read: %w", err)
	}
	if read.MarkedUnread != nil {
		var marked int64
		if *read.MarkedUnread {
			marked = 1
		}
		if err := queries.MarkThreadUnread(ctx, query.MarkThreadUnreadParams{MarkedUnread: marked, ParticipantKey: participant}); err != nil {
			return fmt.Errorf("mark thread unread: %w", err)
		}
	} else {
		if err := queries.ClearDisplayedThreadMarks(ctx, string(ids)); err != nil {
			return fmt.Errorf("clear displayed thread marks: %w", err)
		}
	}
	if err := advanceMessageState(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit thread read: %w", err)
	}
	return nil
}

// DeleteThread atomically removes all SIM conversations for a correspondent.
func (s *Store) DeleteThread(ctx context.Context, id string) error {
	participant, err := threadParticipant(id)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin thread deletion: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Preserve history when any statement fails.
	queries := query.New(tx)
	if err := queries.TombstoneThreadMessages(ctx, participant); err != nil {
		return fmt.Errorf("preserve deleted message fingerprints: %w", err)
	}
	if err := queries.ClearThreadNotifications(ctx, participant); err != nil {
		return fmt.Errorf("clear thread notifications: %w", err)
	}
	if err := queries.DeleteThreadMessages(ctx, participant); err != nil {
		return fmt.Errorf("delete thread messages: %w", err)
	}
	if err := queries.DeleteThreadConversations(ctx, participant); err != nil {
		return fmt.Errorf("delete thread conversations: %w", err)
	}
	if err := advanceMessageState(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit thread deletion: %w", err)
	}
	return nil
}

// ThreadForConversation resolves notifications emitted by older clients and backends.
func (s *Store) ThreadForConversation(ctx context.Context, conversationID string) (string, error) {
	participant, err := query.New(s.db).ThreadParticipant(ctx, strings.TrimSpace(conversationID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve conversation thread: %w", err)
	}
	return ThreadID(participant), nil
}
