package sc3

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

func TestRenderCall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		event notifyevent.CallEvent
		want  content
	}{
		{
			name: "incoming call shows callee number",
			event: notifyevent.CallEvent{
				Modem:    "Office",
				From:     "+8613344445555",
				To:       "+8613344445556",
				Incoming: true,
			},
			want: content{
				Title: "Incoming Call from +86 133 4444 5555",
				Body:  "To: +86 133 4444 5556\nModem: Office\nTime: unknown",
			},
		},
		{
			name:  "unknown callee stays blank",
			event: notifyevent.CallEvent{Modem: "Office", From: "10010", Incoming: true},
			want: content{
				Title: "Incoming Call from 10010",
				Body:  "To: \nModem: Office\nTime: unknown",
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
