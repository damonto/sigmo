// Package automation schedules actions on a specific SIM and modem.
package automation

import (
	"errors"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/app/call"
	"github.com/damonto/sigmo/internal/pkg/message"
)

const (
	maxRepeatDays  = 3650
	minDataBytes   = 1 << 10
	maxDataBytes   = 100 << 20
	maxCallSeconds = 300
)

const (
	actionData = "data"
	actionSMS  = "sms"
	actionCall = "call"

	statusSucceeded   = "succeeded"
	statusFailed      = "failed"
	statusUnconfirmed = "unconfirmed"
	statusInterrupted = "interrupted"
)

// Action describes the one operation performed by a scheduled task.
type Action struct {
	Type            string  `json:"type"`
	Bytes           *int64  `json:"bytes,omitempty"`
	To              *string `json:"to,omitempty"`
	Text            *string `json:"text,omitempty"`
	DurationSeconds *int    `json:"durationSeconds,omitempty"`
}

func (a *Action) validate() error {
	switch a.Type {
	case actionData:
		if a.Bytes == nil || *a.Bytes < minDataBytes || *a.Bytes > maxDataBytes || *a.Bytes%minDataBytes != 0 || a.To != nil || a.Text != nil || a.DurationSeconds != nil {
			return errors.New("provide between 1 KiB and 100 MiB in whole KiB")
		}
	case actionSMS:
		if a.To == nil || a.Text == nil || strings.TrimSpace(*a.Text) == "" || a.Bytes != nil || a.DurationSeconds != nil {
			return errors.New("provide an SMS recipient and message")
		}
		to, err := message.NormalizeSMSAddress(*a.To)
		if err != nil {
			return errors.New("provide a valid SMS recipient")
		}
		a.To = &to
	case actionCall:
		if a.To == nil || a.DurationSeconds == nil || *a.DurationSeconds < 1 || *a.DurationSeconds > maxCallSeconds || a.Bytes != nil || a.Text != nil {
			return errors.New("provide a phone number and a duration from 1 to 300 seconds")
		}
		to, err := call.NormalizePhoneNumber(*a.To)
		if err != nil {
			return errors.New("provide a valid phone number")
		}
		a.To = &to
	default:
		return errors.New("unknown automation action")
	}
	return nil
}

// Schedule is the editable configuration of an automation.
type Schedule struct {
	ScheduledAt time.Time `json:"scheduledAt"`
	RepeatDays  *int      `json:"repeatDays"`
	Action      Action    `json:"action"`
}

func (s *Schedule) validate(now time.Time) error {
	s.ScheduledAt = s.ScheduledAt.UTC().Truncate(time.Millisecond)
	if !s.ScheduledAt.After(now) {
		return errors.New("schedule time must be in the future")
	}
	if s.RepeatDays != nil && (*s.RepeatDays < 1 || *s.RepeatDays > maxRepeatDays) {
		return errors.New("repeat interval must be between 1 and 3650 days")
	}
	return s.Action.validate()
}

// CreateRequest selects the installed SIM and its initial schedule.
type CreateRequest struct {
	ModemID string `json:"modemId"`
	SEID    string `json:"seId,omitempty"`
	Schedule
}

// Task is a line-bound configuration and its execution state.
type Task struct {
	ID        string     `json:"id"`
	ProfileID string     `json:"profileId"`
	ModemID   string     `json:"modemId"`
	SEID      string     `json:"seId,omitempty"`
	NextAt    *time.Time `json:"nextAt"`
	Running   bool       `json:"running"`
	Restoring bool       `json:"restoring"`
	LastRun   *Run       `json:"lastRun"`
	Schedule
}

// TaskSummary describes a configured action and its next run, if scheduled.
type TaskSummary struct {
	ID         string     `json:"id"`
	ProfileID  string     `json:"profileId"`
	SEID       string     `json:"seId,omitempty"`
	ActionType string     `json:"actionType"`
	NextAt     *time.Time `json:"nextAt"`
}

// Run describes the last attempted occurrence and the outcome of its cleanup.
type Run struct {
	ScheduledAt   time.Time `json:"scheduledAt"`
	StartedAt     time.Time `json:"startedAt"`
	FinishedAt    time.Time `json:"finishedAt"`
	Status        string    `json:"status"`
	Code          string    `json:"code,omitempty"`
	RestoreCode   string    `json:"restoreCode,omitempty"`
	BytesReceived *int64    `json:"bytesReceived,omitempty"`
}

type outcome struct {
	Status        string
	Code          string
	BytesReceived *int64
}

func failed(code string) outcome {
	return outcome{Status: statusFailed, Code: code}
}
