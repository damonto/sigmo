package content

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/locale"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

func TestCompose(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.March, 24, 12, 34, 56, 0, time.UTC)
	tests := []struct {
		name string
		ev   notifyevent.Event
		want Message
	}{
		{
			name: "otp shows the code as a copyable field",
			ev:   notifyevent.OTPEvent{Code: " 654321 "},
			want: Message{
				Subject: "Sigmo Login",
				Fields:  []Field{{Label: "Verification code", Value: "654321", Code: true}},
			},
		},
		{
			name: "incoming sms names the sender",
			ev: notifyevent.SMSEvent{
				Modem:    " Office 5G ",
				From:     "+12223334444",
				To:       "+8613344445555",
				Time:     at,
				Text:     " Hi\nthere ",
				Incoming: true,
			},
			want: Message{
				Subject:  "Incoming SMS",
				Headline: "Incoming SMS from +1 (222) 333-4444",
				Fields: []Field{
					{Label: "From", Value: "+1 (222) 333-4444"},
					{Label: "To", Value: "+86 133 4444 5555"},
					{Label: "Modem", Value: "Office 5G"},
					{Label: "Time", Value: "2026-03-24T12:34:56Z"},
				},
				Body: "Hi\nthere",
			},
		},
		{
			name: "outgoing sms names the recipient and marks an empty body",
			ev:   notifyevent.SMSEvent{From: "10086", To: "+12223334444"},
			want: Message{
				Subject:  "Outgoing SMS",
				Headline: "Outgoing SMS to +1 (222) 333-4444",
				Fields: []Field{
					{Label: "From", Value: "10086"},
					{Label: "To", Value: "+1 (222) 333-4444"},
				},
				Body: "(empty message)",
			},
		},
		{
			name: "incoming call omits unknown details",
			ev:   notifyevent.CallEvent{Modem: "Office", From: "10010", Incoming: true},
			want: Message{
				Subject:  "Incoming Call",
				Headline: "Incoming Call from 10010",
				Fields: []Field{
					{Label: "From", Value: "10010"},
					{Label: "Modem", Value: "Office"},
				},
			},
		},
		{
			name: "outgoing call names the callee",
			ev:   notifyevent.CallEvent{From: "+8613344445556", To: "+8613344445555"},
			want: Message{
				Subject:  "Outgoing Call",
				Headline: "Outgoing Call to +86 133 4444 5555",
				Fields: []Field{
					{Label: "From", Value: "+86 133 4444 5556"},
					{Label: "To", Value: "+86 133 4444 5555"},
				},
			},
		},
		{
			name: "call without a counterparty has no headline",
			ev:   notifyevent.CallEvent{Modem: "Office", Incoming: true},
			want: Message{
				Subject: "Incoming Call",
				Fields:  []Field{{Label: "Modem", Value: "Office"}},
			},
		},
		{
			name: "named reminder",
			ev: notifyevent.ReminderEvent{
				ProfileName: "Travel",
				ProfileID:   "8985",
				Modem:       "Office",
				ScheduledAt: at,
				Content:     "Renew",
			},
			want: Message{
				Subject:  "Reminder",
				Headline: "Reminder: Travel",
				Fields: []Field{
					{Label: "Profile", Value: "Travel"},
					{Label: "ICCID", Value: "8985"},
					{Label: "Modem", Value: "Office"},
					{Label: "Time", Value: "2026-03-24T12:34:56Z"},
				},
				Body: "Renew",
			},
		},
		{
			name: "unnamed reminder falls back to the ICCID",
			ev:   notifyevent.ReminderEvent{ProfileID: "8985"},
			want: Message{
				Subject:  "Reminder",
				Headline: "Reminder: 8985",
				Fields:   []Field{{Label: "ICCID", Value: "8985"}},
				Body:     "(empty reminder)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Compose(locale.English, tt.ev)
			if err != nil {
				t.Fatalf("Compose() error = %v", err)
			}
			tt.want.Event = tt.ev
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Compose() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

type unsupportedEvent struct{}

func (unsupportedEvent) Kind() notifyevent.Kind { return "unsupported" }

func TestComposeRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ev   notifyevent.Event
	}{
		{name: "nil event", ev: nil},
		{name: "unsupported event", ev: unsupportedEvent{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Compose(locale.English, tt.ev); err == nil {
				t.Fatal("Compose() error = nil, want error")
			}
		})
	}
}

func TestMessagePlainText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		msg         Message
		wantTitle   string
		wantSummary string
		wantText    string
	}{
		{
			name: "code field without body",
			msg: Message{
				Subject: "Sigmo Login",
				Fields:  []Field{{Label: "Verification code", Value: "654321", Code: true}},
			},
			wantTitle:   "Sigmo Login",
			wantSummary: "Verification code: 654321",
			wantText:    "Sigmo Login\n\nVerification code: 654321",
		},
		{
			name: "headline and details without body",
			msg: Message{
				Subject:  "Incoming Call",
				Headline: "Incoming Call from 10010",
				Fields:   []Field{{Label: "From", Value: "10010"}, {Label: "Modem", Value: "Office"}},
			},
			wantTitle:   "Incoming Call from 10010",
			wantSummary: "From: 10010\nModem: Office",
			wantText:    "Incoming Call\n\nFrom: 10010\nModem: Office",
		},
		{
			name: "body wins the summary",
			msg: Message{
				Subject:  "Incoming SMS",
				Headline: "Incoming SMS from 10086",
				Fields:   []Field{{Label: "From", Value: "10086"}},
				Body:     "Hi\nthere",
			},
			wantTitle:   "Incoming SMS from 10086",
			wantSummary: "Hi\nthere",
			wantText:    "Incoming SMS\n\nFrom: 10086\n\nHi\nthere",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.msg.Title(); got != tt.wantTitle {
				t.Errorf("Title() = %q, want %q", got, tt.wantTitle)
			}
			if got := tt.msg.Summary(); got != tt.wantSummary {
				t.Errorf("Summary() = %q, want %q", got, tt.wantSummary)
			}
			if got := tt.msg.Text(); got != tt.wantText {
				t.Errorf("Text() = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestMessageHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		msg      Message
		contains []string
		excludes []string
	}{
		{
			name: "code field renders as a highlighted box without a details block",
			msg: Message{
				Subject: "Sigmo Login",
				Fields:  []Field{{Label: "Verification code", Value: "654321", Code: true}},
			},
			contains: []string{
				`>Sigmo Login</h1>`,
				`>Verification code</p>`,
				`letter-spacing:0.24em;">654321</div>`,
			},
			excludes: []string{"<strong>", "white-space:pre-wrap"},
		},
		{
			name: "values are escaped",
			msg: Message{
				Subject: "Incoming <SMS>",
				Fields:  []Field{{Label: "From", Value: "A&B"}, {Label: "Modem", Value: "<b>Office</b>"}},
				Body:    "1 < 2\nok",
			},
			contains: []string{
				`>Incoming &lt;SMS&gt;</h1>`,
				`<strong>From:</strong> A&amp;B<br><strong>Modem:</strong> &lt;b&gt;Office&lt;/b&gt;</div>`,
				`white-space:pre-wrap;">1 &lt; 2` + "\n" + `ok</div>`,
			},
			excludes: []string{"<SMS>", "<b>Office</b>"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.msg.HTML()
			if err != nil {
				t.Fatalf("HTML() error = %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("HTML() does not contain %q\ngot: %s", want, got)
				}
			}
			for _, unwanted := range tt.excludes {
				if strings.Contains(got, unwanted) {
					t.Errorf("HTML() contains %q\ngot: %s", unwanted, got)
				}
			}
		})
	}
}

func TestComposeChinese(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.March, 24, 12, 34, 56, 0, time.UTC)
	tests := []struct {
		name string
		ev   notifyevent.Event
		want Message
	}{
		{
			name: "otp",
			ev:   notifyevent.OTPEvent{Code: "654321"},
			want: Message{
				Subject: "Sigmo 登录",
				Fields:  []Field{{Label: "验证码", Value: "654321", Code: true}},
			},
		},
		{
			name: "incoming sms with empty text",
			ev: notifyevent.SMSEvent{
				Modem:    "Office",
				From:     "10086",
				To:       "+8613344445555",
				Time:     at,
				Incoming: true,
			},
			want: Message{
				Subject:  "新短信",
				Headline: "来自 10086 的新短信",
				Fields: []Field{
					{Label: "发件人", Value: "10086"},
					{Label: "收件人", Value: "+86 133 4444 5555"},
					{Label: "Modem", Value: "Office"},
					{Label: "时间", Value: "2026-03-24T12:34:56Z"},
				},
				Body: "（空短信）",
			},
		},
		{
			name: "outgoing call",
			ev:   notifyevent.CallEvent{From: "+8613344445556", To: "10010"},
			want: Message{
				Subject:  "去电",
				Headline: "拨给 10010 的电话",
				Fields: []Field{
					{Label: "主叫", Value: "+86 133 4444 5556"},
					{Label: "被叫", Value: "10010"},
				},
			},
		},
		{
			name: "reminder",
			ev:   notifyevent.ReminderEvent{ProfileName: "Travel", ProfileID: "8985"},
			want: Message{
				Subject:  "提醒",
				Headline: "提醒：Travel",
				Fields: []Field{
					{Label: "Profile", Value: "Travel"},
					{Label: "ICCID", Value: "8985"},
				},
				Body: "（空提醒）",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Compose(locale.Chinese, tt.ev)
			if err != nil {
				t.Fatalf("Compose() error = %v", err)
			}
			tt.want.Event = tt.ev
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Compose() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestComposeWithoutPreferenceUsesEnglish(t *testing.T) {
	t.Parallel()

	for _, lang := range []locale.Tag{"", "fr"} {
		got, err := Compose(lang, notifyevent.OTPEvent{Code: "654321"})
		if err != nil {
			t.Fatalf("Compose(%q) error = %v", lang, err)
		}
		if got.Subject != english.Login {
			t.Errorf("Compose(%q).Subject = %q, want %q", lang, got.Subject, english.Login)
		}
	}
}

// TestPhrasesAreComplete composes every kind of event in every language, so a
// phrase left out of a translation shows up as an empty label or title.
func TestPhrasesAreComplete(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.March, 24, 12, 34, 56, 0, time.UTC)
	events := []notifyevent.Event{
		notifyevent.OTPEvent{Code: "654321"},
		notifyevent.SMSEvent{Modem: "M", From: "10086", To: "10010", Time: at, Incoming: true},
		notifyevent.SMSEvent{Modem: "M", From: "10086", To: "10010", Time: at},
		notifyevent.CallEvent{Modem: "M", From: "10086", To: "10010", Time: at, Incoming: true},
		notifyevent.CallEvent{Modem: "M", From: "10086", To: "10010", Time: at},
		notifyevent.ReminderEvent{ProfileName: "P", ProfileID: "8985", Modem: "M", ScheduledAt: at},
	}
	for _, lang := range []locale.Tag{locale.English, locale.Chinese} {
		for _, ev := range events {
			msg, err := Compose(lang, ev)
			if err != nil {
				t.Fatalf("Compose(%q, %T) error = %v", lang, ev, err)
			}
			if msg.Subject == "" {
				t.Errorf("Compose(%q, %T).Subject is empty", lang, ev)
			}
			if _, isOTP := ev.(notifyevent.OTPEvent); !isOTP && msg.Headline == "" {
				t.Errorf("Compose(%q, %T).Headline is empty", lang, ev)
			}
			if strings.Contains(msg.Headline, "%!") {
				t.Errorf("Compose(%q, %T).Headline = %q, want a filled pattern", lang, ev, msg.Headline)
			}
			for _, f := range msg.Fields {
				if f.Label == "" {
					t.Errorf("Compose(%q, %T) has a field %q without a label", lang, ev, f.Value)
				}
			}
			if msg.Summary() == "" {
				t.Errorf("Compose(%q, %T).Summary() is empty", lang, ev)
			}
			switch ev.(type) {
			case notifyevent.SMSEvent, notifyevent.ReminderEvent:
				// These events carry no text, so Body must be the placeholder.
				if msg.Body == "" {
					t.Errorf("Compose(%q, %T).Body is empty, want a placeholder", lang, ev)
				}
			}
		}
	}
}

func TestPhrasePatternsTakeOneName(t *testing.T) {
	t.Parallel()

	for lang, p := range map[string]phrases{"english": english, "chinese": chinese} {
		patterns := map[string]string{
			"IncomingSMSFrom":  p.IncomingSMSFrom,
			"OutgoingSMSTo":    p.OutgoingSMSTo,
			"IncomingCallFrom": p.IncomingCallFrom,
			"OutgoingCallTo":   p.OutgoingCallTo,
			"ReminderFor":      p.ReminderFor,
		}
		for name, pattern := range patterns {
			if strings.Count(pattern, "%") != 1 || !strings.Contains(pattern, "%s") {
				t.Errorf("%s.%s = %q, want exactly one %%s", lang, name, pattern)
			}
		}
	}
}
