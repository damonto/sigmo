package call

import (
	"context"
	"errors"
	"testing"
	"time"

	pims "github.com/damonto/sigmo/internal/app/ims"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestTimedCallHangsUpItsOwnDialogAfterAnswer(t *testing.T) {
	device := &mmodem.Modem{EquipmentIdentifier: "modem", SIM: &mmodem.SIM{Identifier: "line"}}
	answered := time.Now()
	const duration = 30 * time.Millisecond
	active := pims.VoiceCall{
		ID: "scheduled-call", ModemID: "modem", ProfileID: "line", Route: RouteWiFiCalling,
		Direction: DirectionOutgoing, Number: "+12025550123", State: StateActive,
		StartedAt: answered.Add(-5 * time.Minute), AnsweredAt: answered,
	}
	hangups := 0
	voice := fakeIMSVoice{
		status: pims.VoiceStatus{Connected: true}, voiceCall: active,
		hangup: func(_ context.Context, modem *mmodem.Modem, id string) (pims.VoiceCall, error) {
			hangups++
			if id != active.ID || modem != device || time.Now().Before(answered.Add(duration)) {
				t.Errorf("premature or wrong hangup for dialog %q", id)
			}
			ended := active
			ended.State = StateEnded
			ended.UpdatedAt = time.Now()
			ended.EndedAt = ended.UpdatedAt
			return ended, nil
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	calls := New(testStore(t), voice)
	req := DialRequest{ExpectedProfileID: "line", To: active.Number, Route: RouteWiFiCalling, ClientID: "automation"}
	if err := calls.RunTimedCall(ctx, device, device.SIMIdentity(), req, duration, nil); err != nil {
		t.Fatal(err)
	}
	if hangups != 1 {
		t.Fatalf("hangups = %d, want one", hangups)
	}
}

func TestTimedCallUsesTheRemoteEndTimestamp(t *testing.T) {
	for _, tt := range []struct {
		name      string
		connected time.Duration
		wantErr   error
	}{
		{name: "before deadline", connected: time.Second, wantErr: ErrAutomationEarlyEnd},
		{name: "at deadline", connected: 2 * time.Second},
		{name: "after deadline", connected: 3 * time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			device := &mmodem.Modem{EquipmentIdentifier: "modem", SIM: &mmodem.SIM{Identifier: "line"}}
			answered := time.Now().Add(-time.Minute)
			ended := pims.VoiceCall{
				ID: "scheduled-call", ModemID: "modem", ProfileID: "line", Route: RouteWiFiCalling,
				Direction: DirectionOutgoing, Number: "+12025550123", State: StateEnded,
				StartedAt: answered.Add(-time.Second), AnsweredAt: answered, EndedAt: answered.Add(tt.connected),
			}
			voice := fakeIMSVoice{
				status: pims.VoiceStatus{Connected: true}, voiceCall: ended,
				hangup: func(context.Context, *mmodem.Modem, string) (pims.VoiceCall, error) {
					t.Error("attempted to hang up a terminal dialog")
					return pims.VoiceCall{}, nil
				},
			}
			calls := New(testStore(t), voice)
			req := DialRequest{ExpectedProfileID: "line", To: ended.Number, Route: RouteWiFiCalling, ClientID: "automation"}
			err := calls.RunTimedCall(t.Context(), device, device.SIMIdentity(), req, 2*time.Second, nil)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("RunTimedCall() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestTimedCallReserveTimeoutIsNotAnUnansweredCall(t *testing.T) {
	device := &mmodem.Modem{EquipmentIdentifier: "modem", SIM: &mmodem.SIM{Identifier: "line"}}
	release, err := device.ReserveSIMSlot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	calls := New(testStore(t), nil)
	req := DialRequest{ExpectedProfileID: "line", To: "+12025550123", Route: RouteWiFiCalling}
	err = calls.RunTimedCall(ctx, device, device.SIMIdentity(), req, time.Second, func(context.Context) error {
		t.Error("checked call state without the SIM reservation")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrAutomationUnanswered) {
		t.Fatalf("RunTimedCall() error = %v, want reservation deadline", err)
	}
}
