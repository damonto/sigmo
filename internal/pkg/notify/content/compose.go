package content

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/locale"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

// Compose decides what a notification about ev says, written in lang.
func Compose(lang locale.Tag, ev notifyevent.Event) (Message, error) {
	p := phrasesFor(lang)
	switch ev := ev.(type) {
	case nil:
		return Message{}, errors.New("compose notification: event is required")
	case notifyevent.OTPEvent:
		return Message{
			Event:   ev,
			Subject: p.Login,
			Fields: fields(
				Field{Label: p.VerificationCode, Value: strings.TrimSpace(ev.Code), Code: true},
			),
		}, nil
	case notifyevent.SMSEvent:
		subject, headlinePattern := p.OutgoingSMS, p.OutgoingSMSTo
		if ev.Incoming {
			subject, headlinePattern = p.IncomingSMS, p.IncomingSMSFrom
		}
		return Message{
			Event:    ev,
			Subject:  subject,
			Headline: headline(headlinePattern, ev.DisplayCounterparty()),
			Fields: fields(
				Field{Label: p.Sender, Value: ev.DisplayFrom()},
				Field{Label: p.Recipient, Value: ev.DisplayTo()},
				Field{Label: p.Modem, Value: strings.TrimSpace(ev.Modem)},
				Field{Label: p.Time, Value: timestamp(ev.Time)},
			),
			Body: cmp.Or(strings.TrimSpace(ev.Text), p.EmptySMS),
		}, nil
	case notifyevent.CallEvent:
		subject, headlinePattern := p.OutgoingCall, p.OutgoingCallTo
		if ev.Incoming {
			subject, headlinePattern = p.IncomingCall, p.IncomingCallFrom
		}
		return Message{
			Event:    ev,
			Subject:  subject,
			Headline: headline(headlinePattern, ev.DisplayCounterparty()),
			Fields: fields(
				Field{Label: p.Caller, Value: ev.DisplayFrom()},
				Field{Label: p.Callee, Value: ev.DisplayTo()},
				Field{Label: p.Modem, Value: strings.TrimSpace(ev.Modem)},
				Field{Label: p.Time, Value: timestamp(ev.Time)},
			),
		}, nil
	case notifyevent.ReminderEvent:
		return Message{
			Event:    ev,
			Subject:  p.Reminder,
			Headline: headline(p.ReminderFor, ev.DisplayProfile()),
			Fields: fields(
				// The ICCID row already identifies an unnamed profile.
				Field{Label: p.Profile, Value: strings.TrimSpace(ev.ProfileName)},
				Field{Label: p.ICCID, Value: strings.TrimSpace(ev.ProfileID)},
				Field{Label: p.Modem, Value: strings.TrimSpace(ev.Modem)},
				Field{Label: p.Time, Value: timestamp(ev.ScheduledAt)},
			),
			Body: cmp.Or(strings.TrimSpace(ev.Content), p.EmptyReminder),
		}, nil
	default:
		return Message{}, fmt.Errorf("compose notification for %q: unsupported event", ev.Kind())
	}
}

// fields drops details whose value is unknown, so that no channel shows a
// dangling "To:" line when the modem does not know its own number.
func fields(all ...Field) []Field {
	return slices.DeleteFunc(all, func(f Field) bool { return f.Value == "" })
}

// headline fills pattern with name, or returns "" when there is no name so
// that Title falls back to the subject instead of "Incoming SMS from ".
func headline(pattern, name string) string {
	if name == "" {
		return ""
	}
	return fmt.Sprintf(pattern, name)
}

func timestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
