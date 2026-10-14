package storage

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSIMDisplayUpgrade(t *testing.T) {
	for _, tt := range []struct {
		name    string
		version int
	}{
		{name: "version 8", version: 8},
		{name: "version 9 with missing cache table", version: 9},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sigmo.db")
			s, err := Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := s.Close(); err != nil {
					t.Error(err)
				}
			})
			ctx := t.Context()
			for _, id := range []string{"sim-a", "no-icon", "unknown"} {
				if err := s.RecordSIMProfile(ctx, SIMProfile{ProfileID: id, ModemID: "modem-a", Label: "Work", Number: "+100"}); err != nil {
					t.Fatal(err)
				}
			}
			insertThreadMessage(t, s, "sim-a", "10086", "existing-message")
			contact, err := s.SaveContact(ctx, Contact{Name: "Alice", Notes: "Keep this note"})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SaveCall(ctx, Call{ID: "existing-call", ProfileID: "sim-a", ModemID: "modem-a", Number: "10086", Direction: "outgoing", Route: "wifi_calling", State: "ended", StartedAt: time.Now(), EndedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			call, err := s.Call(ctx, "existing-call")
			if err != nil {
				t.Fatal(err)
			}
			before, err := s.MessagesState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			conversation, err := s.Conversation(ctx, ConversationID("sim-a", "10086"))
			if err != nil {
				t.Fatal(err)
			}
			prepareSIMDisplayUpgrade(t, s, tt.version)
			for range 2 {
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				s, err = Open(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				for _, id := range []string{"sim-a", "no-icon", "unknown"} {
					display, found, err := s.ReadSIMDisplay(ctx, id)
					if err != nil || found {
						t.Fatalf("ReadSIMDisplay(%q) before its first card read = %+v, %v, %v", id, display, found, err)
					}
				}
				after, err := s.MessagesState(ctx)
				if err != nil || after != before {
					t.Fatalf("message state changed: %+v, %v", after, err)
				}
				gotConversation, err := s.Conversation(ctx, conversation.ID)
				if err != nil || !reflect.DeepEqual(gotConversation, conversation) {
					t.Fatalf("conversation changed: %+v, %v", gotConversation, err)
				}
				gotContact, err := s.Contact(ctx, contact.ID)
				if err != nil || !reflect.DeepEqual(gotContact, contact) {
					t.Fatalf("contact changed: %+v, %v", gotContact, err)
				}
				gotCall, err := s.Call(ctx, call.ID)
				if err != nil || !reflect.DeepEqual(gotCall, call) {
					t.Fatalf("call changed: %+v, %v", gotCall, err)
				}
				assertSIMDisplaySchema(t, s)
			}
			if err := s.RecordSIMProfile(ctx, SIMProfile{ProfileID: "sim-a", ModemID: "modem-a", Label: "Personal", Icon: "new-icon", DisplayKnown: true, DisplayRevision: s.SIMDisplayRevision()}); err != nil {
				t.Fatal(err)
			}
			if display, found, err := s.ReadSIMDisplay(ctx, "sim-a"); err != nil || !found || display.Label != "Personal" || display.Icon != "new-icon" {
				t.Fatalf("post-upgrade branding write = %+v, %v, %v", display, found, err)
			}
		})
	}
}

func TestSIMDisplayUpgradeRollsBackAndRetries(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	if err := s.RecordSIMProfile(ctx, SIMProfile{ProfileID: "sim-a", ModemID: "modem-a", Label: "Work"}); err != nil {
		t.Fatal(err)
	}
	prepareSIMDisplayUpgrade(t, s, 9)
	// The second ALTER fails; the first column must roll back with its version record.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE sim_profiles ADD COLUMN display_known INTEGER NOT NULL DEFAULT 0`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("migration succeeded with an injected failure")
	}
	var applied, columns int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE version_id = 10 AND is_applied`).Scan(&applied); err != nil || applied != 0 {
		t.Fatalf("failed migration was recorded: %d, %v", applied, err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('sim_profiles') WHERE name = 'icon'`).Scan(&columns); err != nil || columns != 0 {
		t.Fatalf("failed migration retained columns: %d, %v", columns, err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE sim_profiles DROP COLUMN display_known`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	assertSIMDisplaySchema(t, s)
	if display, found, err := s.ReadSIMDisplay(ctx, "sim-a"); err != nil || found {
		t.Fatalf("retried branding migration = %+v, %v, %v", display, found, err)
	}
}

func prepareSIMDisplayUpgrade(t *testing.T, s *Store, version int) {
	t.Helper()
	for _, statement := range []string{
		`ALTER TABLE sim_profiles DROP COLUMN icon`,
		`ALTER TABLE sim_profiles DROP COLUMN display_known`,
	} {
		if _, err := s.db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.ExecContext(t.Context(), `DELETE FROM goose_db_version WHERE version_id > ?`, version); err != nil {
		t.Fatal(err)
	}
	if version == 9 {
		if _, err := s.db.ExecContext(t.Context(), `INSERT INTO goose_db_version (version_id, is_applied) VALUES (9, 1)`); err != nil {
			t.Fatal(err)
		}
	}
}

func assertSIMDisplaySchema(t *testing.T, s *Store) {
	t.Helper()
	var oldTable, applied int
	if err := s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_schema WHERE name = 'sim_profile_display'`).Scan(&oldTable); err != nil || oldTable != 0 {
		t.Fatalf("obsolete display table remains: %d, %v", oldTable, err)
	}
	if err := s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM goose_db_version WHERE version_id = 10 AND is_applied`).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("migration 10 records = %d, %v", applied, err)
	}
}
