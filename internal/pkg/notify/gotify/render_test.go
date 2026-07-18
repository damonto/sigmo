package gotify

import (
	"testing"
	"time"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

func TestRenderReminder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		event notifyevent.ReminderEvent
		want  content
	}{
		{
			name: "named profile",
			event: notifyevent.ReminderEvent{
				ProfileName: "Travel",
				ScheduledAt: time.Date(2026, 7, 18, 2, 30, 0, 0, time.UTC),
				Content:     "Renew",
			},
			want: content{
				Title: "Reminder: Travel",
				Body:  "Profile: Travel\nTime: 2026-07-18T02:30:00Z\n\nRenew",
			},
		},
		{
			name:  "ICCID fallback",
			event: notifyevent.ReminderEvent{ProfileID: "8985"},
			want: content{
				Title: "Reminder: 8985",
				Body:  "Profile: 8985\nTime: unknown\n\n(empty reminder)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := render(tt.event)
			if err != nil {
				t.Fatalf("render() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("render() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
