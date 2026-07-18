package event

import (
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/phonenumber"
)

type Kind string

const (
	KindOTP      Kind = "otp"
	KindSMS      Kind = "sms"
	KindCall     Kind = "call"
	KindReminder Kind = "reminder"
)

type Event interface {
	Kind() Kind
}

type OTPEvent struct {
	Code string `json:"code"`
}

func (OTPEvent) Kind() Kind {
	return KindOTP
}

type SMSEvent struct {
	ID       string    `json:"-"`
	ModemID  string    `json:"-"`
	Modem    string    `json:"modem"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	Time     time.Time `json:"timestamp,omitempty"`
	Text     string    `json:"text"`
	Incoming bool      `json:"incoming"`
}

func (SMSEvent) Kind() Kind {
	return KindSMS
}

type CallEvent struct {
	ID       string    `json:"-"`
	ModemID  string    `json:"-"`
	Modem    string    `json:"modem"`
	From     string    `json:"from"`
	To       string    `json:"to,omitempty"`
	Time     time.Time `json:"timestamp,omitempty"`
	State    string    `json:"state"`
	Incoming bool      `json:"incoming"`
}

func (CallEvent) Kind() Kind {
	return KindCall
}

type ReminderEvent struct {
	ProfileType string    `json:"profileType"`
	ProfileID   string    `json:"profileId"`
	ProfileName string    `json:"profileName"`
	ModemID     string    `json:"modemId,omitempty"`
	SEID        string    `json:"-"`
	Modem       string    `json:"modem"`
	ScheduledAt time.Time `json:"scheduledAt"`
	Content     string    `json:"content"`
}

func (ReminderEvent) Kind() Kind {
	return KindReminder
}

func (e ReminderEvent) DisplayProfile() string {
	if name := strings.TrimSpace(e.ProfileName); name != "" {
		return name
	}
	return strings.TrimSpace(e.ProfileID)
}

func (e ReminderEvent) DisplayTimestamp() string {
	if e.ScheduledAt.IsZero() {
		return "unknown"
	}
	return e.ScheduledAt.Format(time.RFC3339)
}

func (e ReminderEvent) DisplayContent() string {
	content := strings.TrimSpace(e.Content)
	if content == "" {
		return "(empty reminder)"
	}
	return content
}

func (e CallEvent) DirectionLabel() string {
	if e.Incoming {
		return "Incoming Call"
	}
	return "Outgoing Call"
}

func (e CallEvent) DisplayTimestamp() string {
	if e.Time.IsZero() {
		return "unknown"
	}
	return e.Time.Format(time.RFC3339)
}

func (e CallEvent) Counterparty() string {
	if e.Incoming {
		return strings.TrimSpace(e.From)
	}
	return strings.TrimSpace(e.To)
}

func (e CallEvent) DisplayFrom() string {
	return phonenumber.Display(e.From)
}

func (e CallEvent) DisplayTo() string {
	return phonenumber.Display(e.To)
}

func (e CallEvent) DisplayCounterparty() string {
	if e.Incoming {
		return e.DisplayFrom()
	}
	return e.DisplayTo()
}

func (e SMSEvent) DirectionLabel() string {
	if e.Incoming {
		return "Incoming SMS"
	}
	return "Outgoing SMS"
}

func (e SMSEvent) DisplayText() string {
	text := strings.TrimSpace(e.Text)
	if text == "" {
		return "(empty message)"
	}
	return text
}

func (e SMSEvent) DisplayTimestamp() string {
	if e.Time.IsZero() {
		return "unknown"
	}
	return e.Time.Format(time.RFC3339)
}

func (e SMSEvent) Counterparty() string {
	if e.Incoming {
		return strings.TrimSpace(e.From)
	}
	return strings.TrimSpace(e.To)
}

func (e SMSEvent) DisplayFrom() string {
	return phonenumber.Display(e.From)
}

func (e SMSEvent) DisplayTo() string {
	return phonenumber.Display(e.To)
}

func (e SMSEvent) DisplayCounterparty() string {
	if e.Incoming {
		return e.DisplayFrom()
	}
	return e.DisplayTo()
}
