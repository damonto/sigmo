package ims

import (
	"context"
	"errors"
	"net"
	"slices"
	"time"

	imsgo "github.com/damonto/ims-go"
	"github.com/damonto/ims-go/wfcsetup"
)

const (
	progressModem        = "modem_prepare"
	progressUplink       = "uplink_prepare"
	progressUplinkWait   = "waiting_for_uplink"
	progressPacket       = "lte_packet_service"
	progressVoLTEData    = "volte_data_path"
	progressSIM          = "sim_setup"
	progressCarrierSetup = "websheet_required"
	progressRetry        = "retry_wait"
	maxProgressSteps     = 32
)

// ConnectionProgress describes one connection attempt and its most recent failure.
type ConnectionProgress struct {
	Attempt        uint64                         `json:"attempt" jsonschema:"one-based connection attempt number"`
	Status         imsgo.ConnectionProgressStatus `json:"status" jsonschema:"status of the attempt or step"`
	Stage          string                         `json:"stage" jsonschema:"stable connection stage identifier"`
	StageStartedAt time.Time                      `json:"stageStartedAt" jsonschema:"time when the current stage began"`
	RetryAt        *time.Time                     `json:"retryAt,omitempty" jsonschema:"scheduled retry time when available"`
	Steps          []ConnectionProgressStep       `json:"steps" jsonschema:"recent stages visited in this attempt, in order"`
	LastFailure    *ConnectionProgressFailure     `json:"lastFailure,omitempty" jsonschema:"most recent connection failure, when known"`
}

// ConnectionProgressStep records a stage visited during the attempt.
type ConnectionProgressStep struct {
	Stage     string                         `json:"stage" jsonschema:"stable connection stage identifier"`
	Status    imsgo.ConnectionProgressStatus `json:"status" jsonschema:"status of the attempt or step"`
	StartedAt time.Time                      `json:"startedAt" jsonschema:"time when the step began"`
}

// ConnectionProgressFailure identifies a failed stage without exposing subscriber data.
type ConnectionProgressFailure struct {
	Attempt uint64    `json:"attempt" jsonschema:"one-based connection attempt number"`
	Stage   string    `json:"stage" jsonschema:"stable connection stage identifier"`
	Code    string    `json:"code" jsonschema:"stable reason code for the failure"`
	At      time.Time `json:"at" jsonschema:"time the failure was observed"`
}

func (p *ConnectionProgress) clone() *ConnectionProgress {
	if p == nil {
		return nil
	}
	snapshot := *p
	snapshot.Steps = slices.Clone(p.Steps)
	if p.RetryAt != nil {
		retryAt := *p.RetryAt
		snapshot.RetryAt = &retryAt
	}
	if p.LastFailure != nil {
		failure := *p.LastFailure
		snapshot.LastFailure = &failure
	}
	return &snapshot
}

func (p *ConnectionProgress) finishStep(status imsgo.ConnectionProgressStatus) {
	if i := len(p.Steps) - 1; i >= 0 && (p.Steps[i].Status == imsgo.ProgressRunning || p.Steps[i].Status == imsgo.ProgressWaiting) {
		p.Steps[i].Status = status
	}
}

func (p *ConnectionProgress) enter(stage string, status imsgo.ConnectionProgressStatus, delay time.Duration) {
	if p.Stage == stage && p.Status == status && delay == 0 && p.RetryAt == nil {
		return
	}
	p.finishStep(imsgo.ProgressCompleted)
	p.Stage = stage
	p.Status = status
	p.StageStartedAt = time.Now().UTC()
	p.RetryAt = nil
	if delay > 0 {
		retryAt := p.StageStartedAt.Add(delay)
		p.RetryAt = &retryAt
	}
	p.Steps = append(p.Steps, ConnectionProgressStep{Stage: stage, Status: status, StartedAt: p.StageStartedAt})
	if len(p.Steps) > maxProgressSteps {
		p.Steps = p.Steps[len(p.Steps)-maxProgressSteps:]
	}
}

func (c *coordinator) beginProgress(modemID string, sessionID uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.id != sessionID {
		return
	}
	next := &ConnectionProgress{Attempt: 1}
	if session.progress != nil {
		next.Attempt += session.progress.Attempt
		next.LastFailure = session.progress.LastFailure
	}
	next.enter(progressModem, imsgo.ProgressRunning, 0)
	session.progress = next
	session.progressClient = nil
}

func (c *coordinator) updateProgress(modemID string, sessionID uint64, update func(*ConnectionProgress)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if session := c.sessions[modemID]; session != nil && session.id == sessionID && session.progress != nil {
		update(session.progress)
	}
}

func (c *coordinator) enterProgress(modemID string, sessionID uint64, stage string, status imsgo.ConnectionProgressStatus, delay time.Duration) {
	c.updateProgress(modemID, sessionID, func(progress *ConnectionProgress) {
		progress.enter(stage, status, delay)
	})
}

func (c *coordinator) failProgress(modemID string, sessionID uint64, err error) {
	if err == nil || (errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)) ||
		errors.Is(err, wfcsetup.ErrUserActionRequired) || errors.Is(err, wfcsetup.ErrPending) ||
		errors.Is(err, ErrWiFiCallingUnderlayUnavailable) || errors.Is(err, ErrWebsheetDismissed) {
		return
	}
	c.updateProgress(modemID, sessionID, func(p *ConnectionProgress) {
		p.recordFailure(err)
	})
}

func (p *ConnectionProgress) recordFailure(err error) {
	if p.Status == imsgo.ProgressFailed {
		return
	}
	p.finishStep(imsgo.ProgressFailed)
	p.Status = imsgo.ProgressFailed
	p.RetryAt = nil
	p.LastFailure = &ConnectionProgressFailure{
		Attempt: p.Attempt, Stage: p.Stage, Code: progressFailureCode(err), At: time.Now().UTC(),
	}
}

func progressFailureCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return "timeout"
	}
	switch {
	case errors.Is(err, ErrUnavailable):
		return "access_unavailable"
	case errors.Is(err, wfcsetup.ErrDenied):
		return "carrier_denied"
	default:
		return "connection_failed"
	}
}

// A client is visible to status reads before Connect returns. The connection
// loop owns it throughout setup and freezes its last snapshot on exit.
func (c *coordinator) bindProgressClient(modemID string, sessionID uint64, client *imsgo.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if session := c.sessions[modemID]; session != nil && session.id == sessionID && session.progress != nil {
		session.progressClient = client
	}
}

func (c *coordinator) finishProgressClient(modemID string, sessionID uint64, client *imsgo.Client) {
	snapshot := client.ConnectionProgress()
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.id != sessionID || session.progressClient != client {
		return
	}
	session.progress = combineProgress(session.progress, snapshot)
	session.progressClient = nil
}

func combineProgress(prefix *ConnectionProgress, snapshot imsgo.ConnectionProgress) *ConnectionProgress {
	if snapshot.Attempt == 0 || snapshot.Stage == "" {
		return prefix
	}
	offset := prefix.Attempt - 1
	progress := &ConnectionProgress{
		Attempt: snapshot.Attempt + offset, Status: snapshot.Status, Stage: string(snapshot.Stage),
		StageStartedAt: snapshot.StageStartedAt, RetryAt: snapshot.RetryAt, LastFailure: prefix.LastFailure,
	}
	if snapshot.Attempt == 1 {
		progress.Steps = slices.Clone(prefix.Steps)
		if i := len(progress.Steps) - 1; i >= 0 && len(snapshot.Steps) != 0 &&
			progress.Steps[i].Stage == string(snapshot.Steps[0].Stage) {
			progress.Steps = progress.Steps[:i]
		} else {
			progress.finishStep(imsgo.ProgressCompleted)
		}
	}
	for _, step := range snapshot.Steps {
		progress.Steps = append(progress.Steps, ConnectionProgressStep{
			Stage: string(step.Stage), Status: step.Status, StartedAt: step.StartedAt,
		})
	}
	if len(progress.Steps) > maxProgressSteps {
		progress.Steps = progress.Steps[len(progress.Steps)-maxProgressSteps:]
	}
	if failure := snapshot.LastFailure; failure != nil {
		progress.LastFailure = &ConnectionProgressFailure{
			Attempt: failure.Attempt + offset, Stage: string(failure.Stage), Code: failure.Code, At: failure.At,
		}
	}
	return progress
}
