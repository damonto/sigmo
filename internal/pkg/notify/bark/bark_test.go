package bark

import (
	"testing"
	"time"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

func TestRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ev   notifyevent.Event
		want content
	}{
		{
			name: "otp renders fixed title and body",
			ev:   notifyevent.OTPEvent{Code: "654321"},
			want: content{
				Title: "Sigmo Login",
				Body:  "Your verification code is 654321",
			},
		},
		{
			name: "incoming sms uses sender as title and empty fallback body",
			ev: notifyevent.SMSEvent{
				From:     "+12223334444",
				Incoming: true,
			},
			want: content{
				Title: "+1 (222) 333-4444",
				Body:  "(empty message)",
			},
		},
		{
			name: "incoming call uses caller in title",
			ev: notifyevent.CallEvent{
				Modem:    "Office SIM",
				From:     "+8613344445555",
				Incoming: true,
			},
			want: content{
				Title: "Incoming Call from +86 133 4444 5555",
				Body:  "Modem: Office SIM\nTime: unknown",
			},
		},
		{
			name: "reminder uses profile and content",
			ev: notifyevent.ReminderEvent{
				ProfileName: "Travel",
				ScheduledAt: time.Date(2026, 7, 18, 2, 30, 0, 0, time.UTC),
				Content:     "Renew the plan",
			},
			want: content{
				Title: "Reminder: Travel",
				Body:  "Profile: Travel\nTime: 2026-07-18T02:30:00Z\n\nRenew the plan",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := render(tt.ev)
			if err != nil {
				t.Fatalf("render() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("render() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
