package event

import (
	"strings"
	"time"
)

type Kind string

const (
	KindOTP  Kind = "otp"
	KindSMS  Kind = "sms"
	KindCall Kind = "call"
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
