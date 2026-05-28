package bark

import (
	"testing"

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
				From:     "15550001",
				Incoming: true,
			},
			want: content{
				Title: "15550001",
				Body:  "(empty message)",
			},
		},
		{
			name: "incoming call uses caller in title",
			ev: notifyevent.CallEvent{
				Modem:    "Office SIM",
				From:     "15550002",
				Incoming: true,
			},
			want: content{
				Title: "Incoming Call from 15550002",
				Body:  "Modem: Office SIM\nTime: unknown",
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
