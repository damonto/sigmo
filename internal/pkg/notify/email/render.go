package email

import (
	"fmt"
	"html"
	"strings"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

type content struct {
	Subject  string
	TextBody string
	HTMLBody string
}

func render(ev notifyevent.Event) (content, error) {
	switch ev := ev.(type) {
	case notifyevent.OTPEvent:
		code := strings.TrimSpace(ev.Code)
		return content{
			Subject: "Sigmo Login Verification Code",
			TextBody: fmt.Sprintf(
				"Sigmo Login Verification Code\n\n%s\n\nEnter this code to continue.",
				code,
			),
			HTMLBody: otpHTML(code),
		}, nil
	case notifyevent.SMSEvent:
		target := ev.DisplayCounterparty()
		subject := fmt.Sprintf("Outgoing SMS to %s", target)
		if ev.Incoming {
			subject = fmt.Sprintf("Incoming SMS from %s", target)
		}
		return content{
			Subject: subject,
			TextBody: fmt.Sprintf(
				"%s\n\nFrom : %s\nTo   : %s\nModem: %s\nTime : %s\n\nMessage\n-------\n%s",
				ev.DirectionLabel(),
				ev.DisplayFrom(),
				ev.DisplayTo(),
				strings.TrimSpace(ev.Modem),
				ev.DisplayTimestamp(),
				ev.DisplayText(),
			),
			HTMLBody: smsHTML(ev),
		}, nil
	case notifyevent.CallEvent:
		target := ev.DisplayCounterparty()
		subject := fmt.Sprintf("Outgoing call to %s", target)
		if ev.Incoming {
			subject = fmt.Sprintf("Incoming call from %s", target)
		}
		return content{
			Subject: subject,
			TextBody: fmt.Sprintf(
				"%s\n\nFrom : %s\nModem: %s\nTime : %s",
				ev.DirectionLabel(),
				ev.DisplayFrom(),
				strings.TrimSpace(ev.Modem),
				ev.DisplayTimestamp(),
			),
			HTMLBody: callHTML(ev),
		}, nil
	case notifyevent.ReminderEvent:
		return content{
			Subject: fmt.Sprintf("Reminder: %s", ev.DisplayProfile()),
			TextBody: fmt.Sprintf(
				"Reminder\n\nProfile: %s\nICCID  : %s\nModem  : %s\nTime   : %s\n\n%s",
				ev.DisplayProfile(),
				strings.TrimSpace(ev.ProfileID),
				strings.TrimSpace(ev.Modem),
				ev.DisplayTimestamp(),
				ev.DisplayContent(),
			),
			HTMLBody: reminderHTML(ev),
		}, nil
	default:
		return content{}, fmt.Errorf("rendering email content for %q: unsupported event", ev.Kind())
	}
}

func otpHTML(code string) string {
	return fmt.Sprintf(
		"<div style=\"background:#f5f7fb;padding:24px;font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#111827;\">"+
			"<div style=\"max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #dbe2ea;border-radius:16px;padding:28px;\">"+
			"<p style=\"margin:0 0 8px;color:#6b7280;font-size:12px;letter-spacing:0.08em;text-transform:uppercase;\">Sigmo Login</p>"+
			"<h1 style=\"margin:0 0 18px;font-size:24px;line-height:1.2;\">Verification Code</h1>"+
			"<div style=\"margin:0 0 18px;padding:18px 20px;border:1px solid #dbe2ea;border-radius:12px;background:#f9fafb;text-align:center;font-size:32px;font-weight:700;letter-spacing:0.24em;\">%s</div>"+
			"<p style=\"margin:0;color:#4b5563;font-size:14px;\">Enter this code to continue.</p>"+
			"</div></div>",
		html.EscapeString(code),
	)
}

func smsHTML(ev notifyevent.SMSEvent) string {
	return fmt.Sprintf(
		"<div style=\"background:#f5f7fb;padding:24px;font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#111827;\">"+
			"<div style=\"max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #dbe2ea;border-radius:16px;padding:28px;\">"+
			"<p style=\"margin:0 0 8px;color:#6b7280;font-size:12px;letter-spacing:0.08em;text-transform:uppercase;\">Sigmo Notification</p>"+
			"<h1 style=\"margin:0 0 18px;font-size:24px;line-height:1.2;\">%s</h1>"+
			"<div style=\"margin:0 0 18px;padding:16px 18px;border:1px solid #e5e7eb;border-radius:12px;background:#f9fafb;font-size:14px;line-height:1.7;\">"+
			"<strong>From:</strong> %s<br>"+
			"<strong>To:</strong> %s<br>"+
			"<strong>Modem:</strong> %s<br>"+
			"<strong>Time:</strong> %s"+
			"</div>"+
			"<div style=\"padding:16px 18px;border:1px solid #dbe2ea;border-radius:12px;background:#ffffff;font-size:15px;line-height:1.7;white-space:pre-wrap;\">%s</div>"+
			"</div></div>",
		html.EscapeString(ev.DirectionLabel()),
		html.EscapeString(ev.DisplayFrom()),
		html.EscapeString(ev.DisplayTo()),
		html.EscapeString(strings.TrimSpace(ev.Modem)),
		html.EscapeString(ev.DisplayTimestamp()),
		html.EscapeString(ev.DisplayText()),
	)
}

func callHTML(ev notifyevent.CallEvent) string {
	return fmt.Sprintf(
		"<div style=\"background:#f5f7fb;padding:24px;font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#111827;\">"+
			"<div style=\"max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #dbe2ea;border-radius:16px;padding:28px;\">"+
			"<p style=\"margin:0 0 8px;color:#6b7280;font-size:12px;letter-spacing:0.08em;text-transform:uppercase;\">Sigmo Notification</p>"+
			"<h1 style=\"margin:0 0 18px;font-size:24px;line-height:1.2;\">%s</h1>"+
			"<div style=\"padding:16px 18px;border:1px solid #e5e7eb;border-radius:12px;background:#f9fafb;font-size:14px;line-height:1.7;\">"+
			"<strong>From:</strong> %s<br>"+
			"<strong>Modem:</strong> %s<br>"+
			"<strong>Time:</strong> %s"+
			"</div>"+
			"</div></div>",
		html.EscapeString(ev.DirectionLabel()),
		html.EscapeString(ev.DisplayFrom()),
		html.EscapeString(strings.TrimSpace(ev.Modem)),
		html.EscapeString(ev.DisplayTimestamp()),
	)
}

func reminderHTML(ev notifyevent.ReminderEvent) string {
	return fmt.Sprintf(
		"<div style=\"background:#f5f7fb;padding:24px;font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#111827;\">"+
			"<div style=\"max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #dbe2ea;border-radius:16px;padding:28px;\">"+
			"<p style=\"margin:0 0 8px;color:#6b7280;font-size:12px;letter-spacing:0.08em;text-transform:uppercase;\">Sigmo Reminder</p>"+
			"<h1 style=\"margin:0 0 18px;font-size:24px;line-height:1.2;\">%s</h1>"+
			"<div style=\"margin:0 0 18px;padding:16px 18px;border:1px solid #e5e7eb;border-radius:12px;background:#f9fafb;font-size:14px;line-height:1.7;\">"+
			"<strong>ICCID:</strong> %s<br>"+
			"<strong>Modem:</strong> %s<br>"+
			"<strong>Time:</strong> %s"+
			"</div>"+
			"<div style=\"padding:16px 18px;border:1px solid #dbe2ea;border-radius:12px;background:#ffffff;font-size:15px;line-height:1.7;white-space:pre-wrap;\">%s</div>"+
			"</div></div>",
		html.EscapeString(ev.DisplayProfile()),
		html.EscapeString(strings.TrimSpace(ev.ProfileID)),
		html.EscapeString(strings.TrimSpace(ev.Modem)),
		html.EscapeString(ev.DisplayTimestamp()),
		html.EscapeString(ev.DisplayContent()),
	)
}
