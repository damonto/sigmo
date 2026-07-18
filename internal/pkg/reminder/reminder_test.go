package reminder

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	appvalidator "github.com/damonto/sigmo/internal/pkg/validator"
)

func TestUpdateRequestRecord(t *testing.T) {
	scheduledAt := time.Now().Add(time.Hour).In(time.FixedZone("CST", 8*60*60)).Truncate(time.Second)
	past := time.Now().Add(-time.Minute)
	one := 1
	maximum := maxRepeatDays
	tests := []struct {
		name    string
		request UpdateRequest
		wantErr bool
	}{
		{name: "one shot", request: UpdateRequest{ScheduledAt: scheduledAt, Content: "Top up"}},
		{name: "repeat", request: UpdateRequest{ScheduledAt: scheduledAt, RepeatDays: &one, Content: "Top up"}},
		{name: "maximum repeat", request: UpdateRequest{ScheduledAt: scheduledAt, RepeatDays: &maximum, Content: "Top up"}},
		{name: "missing time", request: UpdateRequest{Content: "Top up"}, wantErr: true},
		{name: "past time", request: UpdateRequest{ScheduledAt: past, Content: "Top up"}, wantErr: true},
		{name: "empty content", request: UpdateRequest{ScheduledAt: scheduledAt, Content: "  "}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.request.Record(ProfileTypePSIM, " iccid ", " modem ", "", " Carrier ")
			if tt.wantErr {
				if err == nil {
					t.Fatal("Record() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Record() error = %v", err)
			}
			if got.ProfileID != "iccid" || got.ModemID != "modem" || got.ProfileName != "Carrier" {
				t.Fatalf("Record() = %+v, want trimmed identity", got)
			}
			if !got.NextAt.Equal(scheduledAt.UTC()) {
				t.Fatalf("Record().NextAt = %v, want %v", got.NextAt, scheduledAt.UTC())
			}
		})
	}
}

func TestUpdateRequestValidation(t *testing.T) {
	one := 1
	maximum := maxRepeatDays
	zero := 0
	negative := -1
	tooLarge := maxRepeatDays + 1
	tests := []struct {
		name       string
		repeatDays *int
		wantErr    bool
	}{
		{name: "empty repeat"},
		{name: "one day", repeatDays: &one},
		{name: "maximum repeat", repeatDays: &maximum},
		{name: "zero repeat", repeatDays: &zero, wantErr: true},
		{name: "negative repeat", repeatDays: &negative, wantErr: true},
		{name: "above maximum", repeatDays: &tooLarge, wantErr: true},
	}
	requestValidator := appvalidator.New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requestValidator.Validate(UpdateRequest{RepeatDays: tt.repeatDays})
			if tt.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestServiceProcessDue(t *testing.T) {
	fixed := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	repeat := 7
	sendErr := errors.New("channel unavailable")
	tests := []struct {
		name        string
		repeatDays  *int
		sendErr     error
		future      bool
		wantSent    int
		wantExists  bool
		wantNextAt  time.Time
		replace     bool
		wantContent string
	}{
		{name: "one shot is removed", wantSent: 1},
		{name: "send error still removes one shot", sendErr: sendErr, wantSent: 1},
		{
			name:       "repeat advances from processing time",
			repeatDays: &repeat,
			wantSent:   1,
			wantExists: true,
			wantNextAt: fixed.Add(7 * 24 * time.Hour),
		},
		{name: "future reminder waits", future: true, wantExists: true},
		{
			name:        "new edit survives delivery",
			wantSent:    1,
			wantExists:  true,
			wantNextAt:  fixed.Add(24 * time.Hour),
			replace:     true,
			wantContent: "Updated",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStore(t)
			scheduler, err := New(store, settings.NewMemoryStore(settings.Default()), nil)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			scheduler.now = func() time.Time { return fixed }
			var sent int
			scheduler.deliver = func(_ context.Context, event notifyevent.ReminderEvent) error {
				sent++
				if event.ProfileID != "iccid" {
					t.Fatalf("event.ProfileID = %q, want iccid", event.ProfileID)
				}
				if tt.replace {
					if err := scheduler.Save(context.Background(), storage.Reminder{
						ProfileType: ProfileTypeESIM.String(),
						ProfileID:   "iccid",
						ModemID:     "modem",
						ProfileName: "Travel",
						NextAt:      fixed.Add(24 * time.Hour),
						Content:     "Updated",
					}); err != nil {
						t.Fatalf("Save(replacement) error = %v", err)
					}
				}
				return tt.sendErr
			}
			nextAt := fixed.Add(-time.Hour)
			if tt.future {
				nextAt = fixed.Add(time.Hour)
			}
			if err := store.UpsertReminder(context.Background(), storage.Reminder{
				ProfileType: ProfileTypeESIM.String(),
				ProfileID:   "iccid",
				ModemID:     "modem",
				ProfileName: "Travel",
				NextAt:      nextAt,
				RepeatDays:  tt.repeatDays,
				Content:     "Renew",
			}); err != nil {
				t.Fatalf("UpsertReminder() error = %v", err)
			}
			if err := scheduler.processDue(context.Background()); err != nil {
				t.Fatalf("processDue() error = %v", err)
			}
			if sent != tt.wantSent {
				t.Fatalf("sent = %d, want %d", sent, tt.wantSent)
			}
			got, exists, err := store.GetReminder(context.Background(), ProfileTypeESIM.String(), "iccid")
			if err != nil {
				t.Fatalf("GetReminder() error = %v", err)
			}
			if exists != tt.wantExists {
				t.Fatalf("exists = %v, want %v", exists, tt.wantExists)
			}
			if !tt.wantNextAt.IsZero() && !got.NextAt.Equal(tt.wantNextAt) {
				t.Fatalf("NextAt = %v, want %v", got.NextAt, tt.wantNextAt)
			}
			if tt.wantContent != "" && got.Content != tt.wantContent {
				t.Fatalf("Content = %q, want %q", got.Content, tt.wantContent)
			}
		})
	}
}

func TestServiceRunStopsWithContext(t *testing.T) {
	scheduler, err := New(openTestStore(t), settings.NewMemoryStore(settings.Default()), nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func openTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}
