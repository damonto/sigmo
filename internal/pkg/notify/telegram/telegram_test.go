package telegram

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
			name: "otp escapes dynamic code only",
			ev:   notifyevent.OTPEvent{Code: "12_34"},
			want: content{
				Text:      "*Sigmo Login*\nVerification code\n\n`12\\_34`",
				ParseMode: parseModeMarkdownV2,
			},
		},
		{
			name: "sms renders markdown with escaped values",
			ev: notifyevent.SMSEvent{
				Modem:    "M_1",
				From:     "+12223334444",
				To:       "+8613344445555",
				Text:     "Hello_world!",
				Incoming: true,
			},
			want: content{
				Text:      "*Incoming SMS*\n\n*From:* \\+1 \\(222\\) 333\\-4444\n*To:* \\+86 133 4444 5555\n*Modem:* M\\_1\n*Time:* unknown\n\n*Message:*\nHello\\_world\\!",
				ParseMode: parseModeMarkdownV2,
			},
		},
		{
			name: "incoming call renders caller and modem",
			ev: notifyevent.CallEvent{
				Modem:    "M_1",
				From:     "+12223334444",
				Incoming: true,
			},
			want: content{
				Text:      "*Incoming Call*\n\n*From:* \\+1 \\(222\\) 333\\-4444\n*Modem:* M\\_1\n*Time:* unknown",
				ParseMode: parseModeMarkdownV2,
			},
		},
		{
			name: "reminder escapes profile and content",
			ev: notifyevent.ReminderEvent{
				ProfileName: "Travel_1",
				ProfileID:   "8985",
				Modem:       "M_1",
				Content:     "Renew!",
			},
			want: content{
				Text:      "*Reminder*\n\n*Profile:* Travel\\_1\n*ICCID:* 8985\n*Modem:* M\\_1\n*Time:* unknown\n\nRenew\\!",
				ParseMode: parseModeMarkdownV2,
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
