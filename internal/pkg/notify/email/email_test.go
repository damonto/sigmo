package email

import (
	"testing"
	"time"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

func TestRender(t *testing.T) {
	t.Parallel()

	timestamp := time.Date(2026, time.March, 24, 12, 34, 56, 0, time.UTC)
	tests := []struct {
		name string
		ev   notifyevent.Event
		want content
	}{
		{
			name: "otp renders text and html bodies",
			ev:   notifyevent.OTPEvent{Code: "654321"},
			want: content{
				Subject:  "Sigmo Login Verification Code",
				TextBody: "Sigmo Login Verification Code\n\n654321\n\nEnter this code to continue.",
				HTMLBody: "<div style=\"background:#f5f7fb;padding:24px;font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#111827;\"><div style=\"max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #dbe2ea;border-radius:16px;padding:28px;\"><p style=\"margin:0 0 8px;color:#6b7280;font-size:12px;letter-spacing:0.08em;text-transform:uppercase;\">Sigmo Login</p><h1 style=\"margin:0 0 18px;font-size:24px;line-height:1.2;\">Verification Code</h1><div style=\"margin:0 0 18px;padding:18px 20px;border:1px solid #dbe2ea;border-radius:12px;background:#f9fafb;text-align:center;font-size:32px;font-weight:700;letter-spacing:0.24em;\">654321</div><p style=\"margin:0;color:#4b5563;font-size:14px;\">Enter this code to continue.</p></div></div>",
			},
		},
		{
			name: "outgoing sms renders fixed subject",
			ev: notifyevent.SMSEvent{
				Modem:    "Office 5G",
				From:     "10086",
				To:       "+12223334444",
				Time:     timestamp,
				Text:     "Hi\nthere",
				Incoming: false,
			},
			want: content{
				Subject:  "Outgoing SMS to +1 (222) 333-4444",
				TextBody: "Outgoing SMS\n\nFrom : 10086\nTo   : +1 (222) 333-4444\nModem: Office 5G\nTime : 2026-03-24T12:34:56Z\n\nMessage\n-------\nHi\nthere",
				HTMLBody: "<div style=\"background:#f5f7fb;padding:24px;font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#111827;\"><div style=\"max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #dbe2ea;border-radius:16px;padding:28px;\"><p style=\"margin:0 0 8px;color:#6b7280;font-size:12px;letter-spacing:0.08em;text-transform:uppercase;\">Sigmo Notification</p><h1 style=\"margin:0 0 18px;font-size:24px;line-height:1.2;\">Outgoing SMS</h1><div style=\"margin:0 0 18px;padding:16px 18px;border:1px solid #e5e7eb;border-radius:12px;background:#f9fafb;font-size:14px;line-height:1.7;\"><strong>From:</strong> 10086<br><strong>To:</strong> +1 (222) 333-4444<br><strong>Modem:</strong> Office 5G<br><strong>Time:</strong> 2026-03-24T12:34:56Z</div><div style=\"padding:16px 18px;border:1px solid #dbe2ea;border-radius:12px;background:#ffffff;font-size:15px;line-height:1.7;white-space:pre-wrap;\">Hi\nthere</div></div></div>",
			},
		},
		{
			name: "incoming call renders caller and modem",
			ev: notifyevent.CallEvent{
				Modem:    "Office 5G",
				From:     "+8613344445555",
				Time:     timestamp,
				Incoming: true,
			},
			want: content{
				Subject:  "Incoming call from +86 133 4444 5555",
				TextBody: "Incoming Call\n\nFrom : +86 133 4444 5555\nModem: Office 5G\nTime : 2026-03-24T12:34:56Z",
				HTMLBody: "<div style=\"background:#f5f7fb;padding:24px;font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#111827;\"><div style=\"max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #dbe2ea;border-radius:16px;padding:28px;\"><p style=\"margin:0 0 8px;color:#6b7280;font-size:12px;letter-spacing:0.08em;text-transform:uppercase;\">Sigmo Notification</p><h1 style=\"margin:0 0 18px;font-size:24px;line-height:1.2;\">Incoming Call</h1><div style=\"padding:16px 18px;border:1px solid #e5e7eb;border-radius:12px;background:#f9fafb;font-size:14px;line-height:1.7;\"><strong>From:</strong> +86 133 4444 5555<br><strong>Modem:</strong> Office 5G<br><strong>Time:</strong> 2026-03-24T12:34:56Z</div></div></div>",
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
