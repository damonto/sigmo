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
