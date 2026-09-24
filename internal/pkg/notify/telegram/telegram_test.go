package telegram

import (
	"testing"

	notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"
)

func TestRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  notifycontent.Message
		want content
	}{
		{
			name: "code field is escaped and set in monospace",
			msg: notifycontent.Message{
				Subject: "Sigmo Login",
				Fields:  []notifycontent.Field{{Label: "Verification code", Value: "12_34", Code: true}},
			},
			want: content{
				Text:      "*Sigmo Login*\n\n*Verification code:* `12\\_34`",
				ParseMode: parseModeMarkdownV2,
			},
		},
		{
			name: "subject, fields and body are escaped",
			msg: notifycontent.Message{
				Subject:  "Incoming SMS",
				Headline: "Incoming SMS from +1 (222) 333-4444",
				Fields: []notifycontent.Field{
					{Label: "From", Value: "+1 (222) 333-4444"},
					{Label: "Modem", Value: "M_1"},
				},
				Body: "Hello_world!",
			},
			want: content{
				Text:      "*Incoming SMS*\n\n*From:* \\+1 \\(222\\) 333\\-4444\n*Modem:* M\\_1\n\nHello\\_world\\!",
				ParseMode: parseModeMarkdownV2,
			},
		},
		{
			name: "subject alone",
			msg:  notifycontent.Message{Subject: "Incoming Call"},
			want: content{Text: "*Incoming Call*", ParseMode: parseModeMarkdownV2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := render(tt.msg); got != tt.want {
				t.Fatalf("render() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
