package ims

import (
	"context"
	"errors"
	"testing"
	"time"

	imsgo "github.com/damonto/ims-go"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	usimcard "github.com/damonto/wwan-go/sim/card"
)

func TestSessionProgressMergesAttemptsAndRejectsOldClient(t *testing.T) {
	const modemID = "modem"
	const sessionID = 1
	session := &sessionState{id: sessionID, phase: sessionPhaseConnecting, profileID: "profile"}
	c := &coordinator{sessions: map[string]*sessionState{modemID: session}}
	c.beginProgress(modemID, sessionID)
	c.enterProgress(modemID, sessionID, progressUplink, imsgo.ProgressRunning, 0)
	c.failProgress(modemID, sessionID, context.DeadlineExceeded)
	c.enterProgress(modemID, sessionID, progressRetry, imsgo.ProgressWaiting, time.Minute)
	before := statusFromSession(true, session, "profile", time.Now()).Progress
	if before.RetryAt == nil || before.LastFailure == nil || before.LastFailure.Stage != progressUplink {
		t.Fatalf("retry progress = %+v, want uplink timeout and a retry deadline", before)
	}

	c.beginProgress(modemID, sessionID)
	c.enterProgress(modemID, sessionID, progressSIM, imsgo.ProgressRunning, 0)
	prefix := session.progress.clone()
	client := &imsgo.Client{}
	c.bindProgressClient(modemID, sessionID, client)
	library := imsgo.ConnectionProgress{
		Attempt: 1,
		Status:  "running",
		Stage:   "ims_register_final",
		Steps: []imsgo.ConnectionStep{
			{Stage: "sim_setup", Status: "completed"},
			{Stage: "ims_register_final", Status: "running"},
		},
	}
	got := combineProgress(prefix, library)
	if got.Attempt != 2 || got.Stage != "ims_register_final" || len(got.Steps) != 3 ||
		got.LastFailure == nil || got.LastFailure.Attempt != 1 {
		t.Fatalf("merged progress = %+v, want second attempt with three steps and the first failure", got)
	}
	if statusFromSession(true, session, "other-profile", time.Now()).Progress != nil {
		t.Fatal("progress was visible for another profile")
	}

	c.beginProgress(modemID, sessionID)
	next := &imsgo.Client{}
	c.bindProgressClient(modemID, sessionID, next)
	c.finishProgressClient(modemID, sessionID, client)
	if session.progressClient != next {
		t.Fatal("old client detached the new client")
	}
	if session.progress.Attempt != 3 || session.progress.Stage != progressModem {
		t.Fatalf("old client changed the new attempt: %+v", session.progress)
	}
}

func TestSessionStatusReadsPendingClientAndRetainsItsFailure(t *testing.T) {
	const sessionID = 1
	modem := &mmodem.Modem{EquipmentIdentifier: "modem", SIM: &mmodem.SIM{Identifier: "profile"}}
	session := &sessionState{id: sessionID, phase: sessionPhaseConnecting, profileID: "profile"}
	c := &coordinator{sessions: map[string]*sessionState{modem.EquipmentIdentifier: session}}
	c.beginProgress(modem.EquipmentIdentifier, sessionID)

	reader := pausedProgressReader{entered: make(chan struct{}), release: make(chan struct{})}
	cfg := &imsgo.Config{
		Terminal: imsgo.TerminalInfo{ID: "356789012345678"},
		Access:   imsgo.VoWiFi(imsgo.VoWiFiConfig{}),
	}
	client, err := imsgo.New(reader, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() = %v", err)
		}
	})
	c.bindProgressClient(modem.EquipmentIdentifier, sessionID, client)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.Connect(ctx) }()
	select {
	case <-reader.entered:
	case err := <-done:
		t.Fatalf("Connect returned before loading SIM: %v", err)
	case <-ctx.Done():
		t.Fatal("Connect did not start loading SIM")
	}
	status, err := c.SessionStatus(ctx, modem, true)
	if err != nil || status.Progress == nil || status.Progress.Stage != progressSIM ||
		status.Progress.Status != imsgo.ProgressRunning || status.Connected {
		t.Fatalf("status during Connect = %+v, %v, want running SIM step", status, err)
	}

	close(reader.release)
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Connect() = %v, want SIM timeout", err)
	}
	c.finishProgressClient(modem.EquipmentIdentifier, sessionID, client)
	status, err = c.SessionStatus(ctx, modem, true)
	if err != nil || status.Progress == nil || status.Progress.LastFailure == nil ||
		status.Progress.LastFailure.Code != "timeout" || status.Progress.LastFailure.Stage != progressSIM {
		t.Fatalf("status after Connect = %+v, %v, want persisted SIM timeout", status, err)
	}
}

type pausedProgressReader struct {
	fakeUSIMReader
	entered chan struct{}
	release chan struct{}
}

func (r pausedProgressReader) ListApplications(ctx context.Context) ([]usimcard.Application, error) {
	close(r.entered)
	select {
	case <-r.release:
		return nil, context.DeadlineExceeded
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
