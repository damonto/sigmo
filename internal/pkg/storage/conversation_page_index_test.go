package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

func TestConversationPagesUseIndexSeeksWithoutSorting(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	for _, tt := range []struct {
		name   string
		read   func(*query.Queries) error
		ranges []string
	}{
		{
			name: "first page",
			read: func(q *query.Queries) error {
				_, err := q.ListConversationMessagePage(ctx, query.ListConversationMessagePageParams{
					ConversationID: "conversation", Snapshot: 100000, PageLimit: 51,
				})
				return err
			},
			ranges: []string{"conversation_id=?"},
		},
		{
			name: "equal timestamp IDs",
			read: func(q *query.Queries) error {
				_, err := q.ListConversationMessagesAtTime(ctx, query.ListConversationMessagesAtTimeParams{
					ConversationID: "conversation", Snapshot: 100000, PageLimit: 51,
					CursorTimestamp: "2026-10-15T00:00:00Z", CursorID: 50000,
				})
				return err
			},
			ranges: []string{"<expr>=?", "id<?"},
		},
		{
			name: "earlier timestamps",
			read: func(q *query.Queries) error {
				_, err := q.ListConversationMessagesBeforeTime(ctx, query.ListConversationMessagesBeforeTimeParams{
					ConversationID: "conversation", Snapshot: 100000, PageLimit: 51,
					CursorTimestamp: "2026-10-15T00:00:00Z",
				})
				return err
			},
			ranges: []string{"<expr><?"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Capture the generated statement so the plan tests the same SQL the
			// application executes, with the production SQLite driver and schema.
			capture := &conversationQueryCapture{DBTX: s.db}
			if err := tt.read(query.New(capture)); err != nil {
				t.Fatal(err)
			}
			rows, err := s.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+capture.statement, capture.args...)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rows.Close() }() // Rows.Err reports iteration errors below.
			var details []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				details = append(details, detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			plan := strings.Join(details, "\n")
			t.Log(plan)
			if !strings.Contains(plan, "SEARCH messages USING INDEX idx_messages_conversation_time") || strings.Contains(plan, "TEMP B-TREE") {
				t.Fatalf("page must use its ordered index without sorting:\n%s", plan)
			}
			for _, bound := range tt.ranges {
				if !strings.Contains(plan, bound) {
					t.Errorf("page must seek to %q:\n%s", bound, plan)
				}
			}
		})
	}
}

type conversationQueryCapture struct {
	query.DBTX
	statement string
	args      []any
}

func (c *conversationQueryCapture) QueryContext(ctx context.Context, statement string, args ...any) (*sql.Rows, error) {
	c.statement, c.args = statement, args
	return c.DBTX.QueryContext(ctx, statement, args...)
}

func TestConversationHistoryIndexUpgradeRollsBackAndPreservesData(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, key := range []string{"first", "second", "third"} {
		insertThreadMessage(t, s, "sim", "10086", key)
	}
	id := ConversationID("sim", "10086")
	watermark := int64(1)
	if err := s.UpdateConversationRead(ctx, ReadUpdate{ConversationID: id, ReadThroughMessageID: &watermark}); err != nil {
		t.Fatal(err)
	}
	before, err := s.ConversationMessagesPage(ctx, id, HistoryFilter{Limit: 2})
	if err != nil || before.NextCursor == "" {
		t.Fatalf("initial page = %+v, %v", before, err)
	}
	older, err := s.ConversationMessagesPage(ctx, id, HistoryFilter{Limit: 2, Cursor: before.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	// Recreate version 10, then reject the ledger write after CREATE INDEX.
	for _, statement := range []string{
		`DROP INDEX idx_messages_conversation_time`,
		`DELETE FROM goose_db_version WHERE version_id = 11`,
		`CREATE TRIGGER reject_history_index BEFORE INSERT ON goose_db_version
		 WHEN NEW.version_id = 11 BEGIN SELECT RAISE(ABORT, 'injected index migration failure'); END`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("index migration succeeded with a rejected version record")
	}
	var indexes, versions int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE name = 'idx_messages_conversation_time'`).Scan(&indexes); err != nil || indexes != 0 {
		t.Fatalf("failed migration retained index = %d, %v", indexes, err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE version_id = 11`).Scan(&versions); err != nil || versions != 0 {
		t.Fatalf("failed migration retained version = %d, %v", versions, err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP TRIGGER reject_history_index`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		s, err = Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		after, err := s.ConversationMessagesPage(ctx, id, HistoryFilter{Limit: 2})
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatalf("upgrade changed history or read state: %+v, %v; want %+v", after, err, before)
		}
		afterOlder, err := s.ConversationMessagesPage(ctx, id, HistoryFilter{Limit: 2, Cursor: before.NextCursor})
		if err != nil || !reflect.DeepEqual(afterOlder, older) {
			t.Fatalf("upgrade invalidated the cursor: %+v, %v; want %+v", afterOlder, err, older)
		}
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE version_id = 11 AND is_applied`).Scan(&versions); err != nil || versions != 1 {
			t.Fatalf("index migration records = %d, %v", versions, err)
		}
	}
}
