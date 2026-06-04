package call

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/websheet"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

func TestDialRejectsInvalidRequestsBeforeRouting(t *testing.T) {
	tests := []struct {
		name    string
		number  string
		route   string
		wantErr error
	}{
		{name: "number required", number: "", route: RouteAuto, wantErr: ErrNumberRequired},
		{name: "invalid route", number: "+12242255559", route: "satellite", wantErr: ErrInvalidRoute},
		{name: "star ussd uses ussd api", number: "*123#", route: RouteAuto, wantErr: ErrUSSDDialString},
		{name: "hash ussd uses ussd api", number: "#123", route: RouteModem, wantErr: ErrUSSDDialString},
		{name: "auto route has no connected backend", number: "+12242255559", route: RouteAuto, wantErr: ErrNoRouteAvailable},
		{name: "wifi calling route disconnected", number: "+12242255559", route: RouteWiFiCalling, wantErr: ErrWiFiCallingNotConnected},
		{name: "modem route unavailable", number: "+12242255559", route: RouteModem, wantErr: ErrModemCallingUnavailable},
	}

	service := New(nil, fakeWiFiCalling{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Dial(context.Background(), nil, tt.number, tt.route)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Dial() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestDialMapsBackendDisconnectedAfterRouteSelected(t *testing.T) {
	service := New(nil, fakeWiFiCalling{
		status:  wificalling.Status{Connected: true},
		dialErr: wificalling.ErrNotConnected,
	})

	_, err := service.Dial(context.Background(), nil, "+12242255559", RouteAuto)
	if !errors.Is(err, ErrWiFiCallingNotConnected) {
		t.Fatalf("Dial() error = %v, want %v", err, ErrWiFiCallingNotConnected)
	}
}

func TestNormalizeDialString(t *testing.T) {
	tests := []struct {
		name    string
		number  string
		want    string
		wantErr error
	}{
		{name: "compacts formatted international dial string", number: " +1 (224) 225-5559 ", want: "+12242255559"},
		{name: "keeps local dial string", number: "2242255559", want: "2242255559"},
		{name: "keeps short code", number: "777", want: "777"},
		{name: "keeps 011 international access dial string", number: "0118613800138000", want: "0118613800138000"},
		{name: "keeps formatted 011 international access dial string", number: "011 86 138 0013 8000", want: "0118613800138000"},
		{name: "keeps carrier service prefix dial string", number: "12583113788889999", want: "12583113788889999"},
		{name: "rejects lone plus", number: "+", wantErr: ErrInvalidNumber},
		{name: "rejects letters", number: "12583A13788889999", wantErr: ErrInvalidNumber},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeDialString(tt.number)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("normalizeDialString() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeDialString() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeDialString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunPersistsAndPublishesWiFiCallingVoiceEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	subscriberCh := make(chan wificalling.VoiceEventFunc, 1)
	service := New(store, fakeWiFiCalling{
		subscribe: func(fn wificalling.VoiceEventFunc) func() {
			subscriberCh <- fn
			return func() {}
		},
	})
	events, unsubscribe := service.Subscribe(1)
	defer unsubscribe()

	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx)
	}()
	var subscriber wificalling.VoiceEventFunc
	select {
	case subscriber = <-subscriberCh:
	case <-time.After(time.Second):
		t.Fatal("SubscribeVoiceEvents was not called")
	}

	voiceCall := wificalling.VoiceCall{
		ID:        "call-1",
		ProfileID: "profile-a",
		ModemID:   "modem-1",
		Direction: DirectionIncoming,
		Number:    "+12242255559",
		State:     StateRinging,
		StartedAt: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
	}
	subscriber(wificalling.VoiceEvent{Call: voiceCall})

	stored, err := store.GetCall(ctx, "call-1")
	if err != nil {
		t.Fatalf("GetCall() error = %v", err)
	}
	if stored.Route != RouteWiFiCalling || stored.State != StateRinging || stored.Number != "+12242255559" || stored.Hold != HoldNone {
		t.Fatalf("stored call = %+v, want Wi-Fi Calling ringing call", stored)
	}

	select {
	case event := <-events:
		if event.Call.ID != "call-1" || event.Call.Route != RouteWiFiCalling {
			t.Fatalf("published event = %+v, want call-1 over Wi-Fi Calling", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for call event")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunPublishesWiFiCallingVoiceEventsWhenPersistenceFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := testStore(t)
	subscriberCh := make(chan wificalling.VoiceEventFunc, 1)
	service := New(store, fakeWiFiCalling{
		subscribe: func(fn wificalling.VoiceEventFunc) func() {
			subscriberCh <- fn
			return func() {}
		},
	})
	events, unsubscribe := service.Subscribe(1)
	defer unsubscribe()

	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx)
	}()
	var subscriber wificalling.VoiceEventFunc
	select {
	case subscriber = <-subscriberCh:
	case <-time.After(time.Second):
		t.Fatal("SubscribeVoiceEvents was not called")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	subscriber(wificalling.VoiceEvent{Call: wificalling.VoiceCall{
		ID:        "call-after-store-close",
		ProfileID: "profile-a",
		ModemID:   "modem-1",
		Direction: DirectionIncoming,
		Number:    "+12242255559",
		State:     StateRinging,
		StartedAt: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
	}})

	select {
	case event := <-events:
		if event.Call.ID != "call-after-store-close" {
			t.Fatalf("published event = %+v, want call-after-store-close", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for call event")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestDialPersistsRouteAndPublishesEvent(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	service := New(store, fakeWiFiCalling{
		status: wificalling.Status{Connected: true},
		voiceCall: wificalling.VoiceCall{
			ID:        "call-2",
			ProfileID: "profile-a",
			ModemID:   "modem-1",
			Direction: DirectionOutgoing,
			Number:    "+12242255559",
			State:     StateDialing,
			StartedAt: time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC),
		},
	})
	events, unsubscribe := service.Subscribe(1)
	defer unsubscribe()

	call, err := service.Dial(ctx, nil, " +12242255559 ", RouteAuto)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	if call.ID != "call-2" || call.Route != RouteWiFiCalling || call.Number != "+12242255559" {
		t.Fatalf("Dial() = %+v, want persisted Wi-Fi Calling call", call)
	}

	stored, err := store.GetCall(ctx, "call-2")
	if err != nil {
		t.Fatalf("GetCall() error = %v", err)
	}
	if stored.Route != RouteWiFiCalling || stored.Direction != DirectionOutgoing {
		t.Fatalf("stored call = %+v, want outgoing Wi-Fi Calling call", stored)
	}

	select {
	case event := <-events:
		if event.Call.ID != "call-2" {
			t.Fatalf("event call id = %q, want call-2", event.Call.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dial event")
	}
}

func TestDialPersistsSelectedRouteFailure(t *testing.T) {
	tests := []struct {
		name      string
		voiceCall wificalling.VoiceCall
		dialErr   error
		wantErr   string
	}{
		{
			name: "wifi calling setup fails after route selection",
			voiceCall: wificalling.VoiceCall{
				ID:        "failed-call-1",
				ProfileID: "profile-a",
				ModemID:   "modem-1",
				Direction: DirectionOutgoing,
				Number:    "+12242255559",
				State:     StateFailed,
				Reason:    "sip rejected",
				StartedAt: time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
				EndedAt:   time.Date(2026, 5, 27, 12, 0, 1, 0, time.UTC),
				UpdatedAt: time.Date(2026, 5, 27, 12, 0, 1, 0, time.UTC),
			},
			dialErr: errors.New("sip rejected"),
			wantErr: "dial Wi-Fi Calling: sip rejected",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := testStore(t)
			service := New(store, fakeWiFiCalling{
				status:    wificalling.Status{Connected: true},
				voiceCall: tt.voiceCall,
				dialErr:   tt.dialErr,
			})
			events, unsubscribe := service.Subscribe(1)
			defer unsubscribe()

			_, err := service.Dial(ctx, nil, "+12242255559", RouteAuto)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Dial() error = %v, want %q", err, tt.wantErr)
			}

			stored, err := store.GetCall(ctx, tt.voiceCall.ID)
			if err != nil {
				t.Fatalf("GetCall() error = %v", err)
			}
			if stored.State != StateFailed || stored.Reason != "sip rejected" || stored.Route != RouteWiFiCalling {
				t.Fatalf("stored call = %+v, want failed Wi-Fi Calling call", stored)
			}

			select {
			case event := <-events:
				if event.Call.ID != tt.voiceCall.ID || event.Call.State != StateFailed {
					t.Fatalf("event = %+v, want failed call event", event)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for failed dial event")
			}
		})
	}
}

func TestMapWiFiCallingMediaError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
		want    string
	}{
		{name: "nil", err: nil, wantErr: nil},
		{name: "unsupported codec", err: wificalling.ErrUnsupportedCodec, wantErr: ErrUnsupportedCodec},
		{name: "media unavailable", err: wificalling.ErrUnavailable, wantErr: ErrMediaUnavailable},
		{name: "wifi calling disconnected", err: wificalling.ErrNotConnected, wantErr: ErrWiFiCallingNotConnected},
		{name: "unexpected", err: errors.New("rtp transport"), want: "open Wi-Fi Calling media: rtp transport"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapWiFiCallingMediaError(tt.err)
			if tt.want != "" {
				if err == nil || err.Error() != tt.want {
					t.Fatalf("mapWiFiCallingMediaError() error = %v, want %q", err, tt.want)
				}
				return
			}
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("mapWiFiCallingMediaError() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("mapWiFiCallingMediaError() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestEndUnavailableWiFiCallingMediaClosesStoredCall(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	service := New(store, fakeWiFiCalling{})
	events, unsubscribe := service.Subscribe(1)
	defer unsubscribe()

	startedAt := time.Date(2026, 5, 28, 13, 10, 0, 0, time.UTC)
	call := storage.Call{
		ID:        "call-media-gone",
		ProfileID: "profile-a",
		ModemID:   "modem-1",
		Route:     RouteWiFiCalling,
		Direction: DirectionIncoming,
		Number:    "+12242255559",
		State:     StateActive,
		StartedAt: startedAt,
		UpdatedAt: startedAt,
	}
	if err := store.SaveCall(ctx, call); err != nil {
		t.Fatalf("SaveCall() error = %v", err)
	}

	service.endUnavailableWiFiCallingMedia(ctx, call)

	stored, err := store.GetCall(ctx, call.ID)
	if err != nil {
		t.Fatalf("GetCall() error = %v", err)
	}
	if stored.State != StateEnded || stored.Reason != ErrMediaUnavailable.Error() {
		t.Fatalf("stored call state = %q/%q, want ended/media unavailable", stored.State, stored.Reason)
	}
	if stored.EndedAt.IsZero() || stored.UpdatedAt.Before(startedAt) {
		t.Fatalf("stored call times = ended %v updated %v, want closed after %v", stored.EndedAt, stored.UpdatedAt, startedAt)
	}

	select {
	case event := <-events:
		if event.Call.ID != call.ID || event.Call.State != StateEnded {
			t.Fatalf("event = %+v, want ended call", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ended call event")
	}
}

func TestEndUnavailableWiFiCallingMediaIgnoresTerminalCall(t *testing.T) {
	store := testStore(t)
	service := New(store, fakeWiFiCalling{})
	events, unsubscribe := service.Subscribe(1)
	defer unsubscribe()

	service.endUnavailableWiFiCallingMedia(context.Background(), storage.Call{
		ID:        "call-ended",
		ProfileID: "profile-a",
		ModemID:   "modem-1",
		Route:     RouteWiFiCalling,
		Direction: DirectionIncoming,
		Number:    "+12242255559",
		State:     StateEnded,
	})

	select {
	case event := <-events:
		t.Fatalf("event = %+v, want no event for terminal call", event)
	default:
	}
}

func TestUpdateRejectsUnsupportedState(t *testing.T) {
	service := New(nil, fakeWiFiCalling{})
	_, err := service.Update(context.Background(), nil, "call-1", UpdateRequest{State: StateFailed})
	if !errors.Is(err, ErrInvalidCallState) {
		t.Fatalf("Update() error = %v, want %v", err, ErrInvalidCallState)
	}
}

func TestUpdateRejectsStateAndHoldTogether(t *testing.T) {
	service := New(nil, fakeWiFiCalling{})
	_, err := service.Update(context.Background(), nil, "call-1", UpdateRequest{State: StateActive, Hold: HoldLocal})
	if !errors.Is(err, ErrCallUpdateConflict) {
		t.Fatalf("Update() error = %v, want %v", err, ErrCallUpdateConflict)
	}
}

func TestSetHoldRejectsInvalidHold(t *testing.T) {
	service := New(nil, fakeWiFiCalling{})
	_, err := service.SetHold(context.Background(), nil, "call-1", HoldRemote)
	if !errors.Is(err, ErrInvalidCallHold) {
		t.Fatalf("SetHold() error = %v, want %v", err, ErrInvalidCallHold)
	}
}

func TestSendDTMFRejectsInvalidDigitsBeforeLookup(t *testing.T) {
	tests := []struct {
		name    string
		digits  string
		wantErr error
	}{
		{name: "empty", wantErr: ErrDTMFDigitsRequired},
		{name: "whitespace", digits: "  ", wantErr: ErrDTMFDigitsRequired},
		{name: "letter outside dtmf range", digits: "12x", wantErr: ErrInvalidDTMFDigit},
		{name: "unicode digit", digits: "１", wantErr: ErrInvalidDTMFDigit},
	}

	service := New(nil, fakeWiFiCalling{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.SendDTMF(context.Background(), nil, "call-1", tt.digits)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SendDTMF() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidDTMFDigits(t *testing.T) {
	tests := []struct {
		name   string
		digits string
		want   bool
	}{
		{name: "numeric star pound", digits: "123*0#", want: true},
		{name: "upper abcd", digits: "ABCD", want: true},
		{name: "lower abcd", digits: "abcd", want: true},
		{name: "empty", want: true},
		{name: "invalid letter", digits: "E", want: false},
		{name: "plus", digits: "+", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validDTMFDigits(tt.digits); got != tt.want {
				t.Fatalf("validDTMFDigits(%q) = %v, want %v", tt.digits, got, tt.want)
			}
		})
	}
}

func TestSendDTMFValidatesStoredCallAndRoutesToWiFiCalling(t *testing.T) {
	baseCall := storage.Call{
		ID:        "call-1",
		ProfileID: "profile-a",
		ModemID:   "modem-1",
		Route:     RouteWiFiCalling,
		Direction: DirectionOutgoing,
		Number:    "+12242255559",
		State:     StateActive,
		Hold:      HoldNone,
		StartedAt: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
	}
	tests := []struct {
		name       string
		call       *storage.Call
		digits     string
		wantErr    error
		wantCalled bool
	}{
		{name: "call not found", digits: "1", wantErr: ErrCallNotFound},
		{name: "modem route unavailable", call: callWith(baseCall, func(call *storage.Call) { call.Route = RouteModem }), digits: "1", wantErr: ErrModemCallingUnavailable},
		{name: "local hold blocks dtmf", call: callWith(baseCall, func(call *storage.Call) { call.Hold = HoldLocal }), digits: "1", wantErr: ErrCallOnHold},
		{name: "local remote hold blocks dtmf", call: callWith(baseCall, func(call *storage.Call) { call.Hold = HoldLocalRemote }), digits: "1", wantErr: ErrCallOnHold},
		{name: "ended state blocks dtmf", call: callWith(baseCall, func(call *storage.Call) { call.State = StateEnded }), digits: "1", wantErr: ErrInvalidDTMFCallState},
		{name: "early media can send dtmf", call: callWith(baseCall, func(call *storage.Call) { call.State = StateEarlyMedia }), digits: "1", wantCalled: true},
		{name: "active can send dtmf", call: &baseCall, digits: "*#", wantCalled: true},
		{name: "confirmed can send dtmf", call: callWith(baseCall, func(call *storage.Call) { call.State = StateConfirmed }), digits: "A", wantCalled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := testStore(t)
			if tt.call != nil {
				if err := store.SaveCall(ctx, *tt.call); err != nil {
					t.Fatalf("SaveCall() error = %v", err)
				}
			}
			called := false
			service := New(store, fakeWiFiCalling{
				sendDTMF: func(callID string, digits string) error {
					called = true
					if callID != baseCall.ID || digits != tt.digits {
						t.Fatalf("SendCallDTMF() = callID %q digits %q, want %q/%q", callID, digits, baseCall.ID, tt.digits)
					}
					return nil
				},
			})
			modem := &mmodem.Modem{
				EquipmentIdentifier: baseCall.ModemID,
				Sim:                 &mmodem.SIM{Identifier: baseCall.ProfileID},
			}

			err := service.SendDTMF(ctx, modem, baseCall.ID, tt.digits)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("SendDTMF() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("SendDTMF() error = %v", err)
			}
			if called != tt.wantCalled {
				t.Fatalf("SendCallDTMF called = %v, want %v", called, tt.wantCalled)
			}
		})
	}
}

func TestSendDTMFMapsWiFiCallingError(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	call := storage.Call{
		ID:        "call-1",
		ProfileID: "profile-a",
		ModemID:   "modem-1",
		Route:     RouteWiFiCalling,
		Direction: DirectionOutgoing,
		Number:    "+12242255559",
		State:     StateActive,
		Hold:      HoldNone,
		StartedAt: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
	}
	if err := store.SaveCall(ctx, call); err != nil {
		t.Fatalf("SaveCall() error = %v", err)
	}
	service := New(store, fakeWiFiCalling{dtmfErr: wificalling.ErrNotConnected})
	modem := &mmodem.Modem{
		EquipmentIdentifier: call.ModemID,
		Sim:                 &mmodem.SIM{Identifier: call.ProfileID},
	}

	err := service.SendDTMF(ctx, modem, call.ID, "1")
	if !errors.Is(err, ErrWiFiCallingNotConnected) {
		t.Fatalf("SendDTMF() error = %v, want %v", err, ErrWiFiCallingNotConnected)
	}
}

func callWith(call storage.Call, update func(*storage.Call)) *storage.Call {
	update(&call)
	return &call
}

func TestDeleteRemovesTerminalCallRecords(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	service := New(store, fakeWiFiCalling{})
	call := storage.Call{
		ID:        "call-ended",
		ProfileID: "profile-a",
		ModemID:   "modem-1",
		Route:     RouteWiFiCalling,
		Direction: DirectionOutgoing,
		Number:    "+12242255559",
		State:     StateEnded,
		StartedAt: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
		EndedAt:   time.Date(2026, 5, 27, 10, 1, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 27, 10, 1, 0, 0, time.UTC),
	}
	if err := store.SaveCall(ctx, call); err != nil {
		t.Fatalf("SaveCall() error = %v", err)
	}

	if err := service.deleteCall(ctx, call); err != nil {
		t.Fatalf("deleteCall() error = %v", err)
	}
	if _, err := store.GetCall(ctx, call.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetCall(deleted) error = %v, want %v", err, storage.ErrNotFound)
	}
}

func TestDeleteRejectsActiveCallRecords(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	service := New(store, fakeWiFiCalling{})
	call := storage.Call{
		ID:        "call-active",
		ProfileID: "profile-a",
		ModemID:   "modem-1",
		Route:     RouteWiFiCalling,
		Direction: DirectionOutgoing,
		Number:    "+12242255559",
		State:     StateActive,
		StartedAt: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
	}
	if err := store.SaveCall(ctx, call); err != nil {
		t.Fatalf("SaveCall() error = %v", err)
	}

	err := service.deleteCall(ctx, call)
	if !errors.Is(err, ErrCallRecordActive) {
		t.Fatalf("deleteCall() error = %v, want %v", err, ErrCallRecordActive)
	}
	if _, err := store.GetCall(ctx, call.ID); err != nil {
		t.Fatalf("GetCall(active) error = %v", err)
	}
}

func TestSubscribeUnsubscribeLeavesChannelOpen(t *testing.T) {
	service := New(nil, fakeWiFiCalling{})
	events, unsubscribe := service.Subscribe(1)
	unsubscribe()
	service.publish(Event{Call: storage.Call{ID: "call-1"}})

	select {
	case _, ok := <-events:
		if !ok {
			t.Fatal("Subscribe() channel was closed")
		}
	default:
	}
}

func TestMapWiFiCallingActionError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
		want    string
	}{
		{name: "nil", err: nil, wantErr: nil},
		{name: "wifi calling disconnected", err: wificalling.ErrNotConnected, wantErr: ErrWiFiCallingNotConnected},
		{name: "call unavailable", err: wificalling.ErrUnavailable, wantErr: ErrCallNotFound},
		{name: "dtmf unsupported", err: wificalling.ErrUnsupportedDTMF, wantErr: ErrUnsupportedDTMF},
		{name: "unexpected", err: errors.New("sip transaction"), want: "answer Wi-Fi Calling: sip transaction"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapWiFiCallingActionError("answer", tt.err)
			if tt.want != "" {
				if err == nil || err.Error() != tt.want {
					t.Fatalf("mapWiFiCallingActionError() error = %v, want %q", err, tt.want)
				}
				return
			}
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("mapWiFiCallingActionError() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("mapWiFiCallingActionError() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

type fakeWiFiCalling struct {
	status     wificalling.Status
	dialErr    error
	voiceCall  wificalling.VoiceCall
	answerCall wificalling.VoiceCall
	rejectCall wificalling.VoiceCall
	hangupCall wificalling.VoiceCall
	holdCall   wificalling.VoiceCall
	resumeCall wificalling.VoiceCall
	dtmfErr    error
	sendDTMF   func(string, string) error
	mediaErr   error
	subscribe  func(wificalling.VoiceEventFunc) func()
}

func (fakeWiFiCalling) Run(context.Context, *mmodem.Registry) error { return nil }
func (fakeWiFiCalling) Settings(context.Context, *mmodem.Modem) (wificalling.Settings, error) {
	return wificalling.Settings{}, nil
}
func (fakeWiFiCalling) UpdateSettings(context.Context, *mmodem.Modem, wificalling.Settings) error {
	return nil
}
func (f fakeWiFiCalling) Status(context.Context, *mmodem.Modem) (wificalling.Status, error) {
	return f.status, nil
}
func (fakeWiFiCalling) EmergencyAddressUpdateAvailable(context.Context, *mmodem.Modem) bool {
	return false
}
func (fakeWiFiCalling) StartWebsheet(context.Context, *mmodem.Modem) (websheet.Info, error) {
	return websheet.Info{}, nil
}
func (fakeWiFiCalling) StartEmergencyAddressUpdate(context.Context, *mmodem.Modem) (websheet.Info, error) {
	return websheet.Info{}, nil
}
func (fakeWiFiCalling) SendSMS(context.Context, *mmodem.Modem, string, string) (storage.Message, error) {
	return storage.Message{}, nil
}
func (fakeWiFiCalling) ApplyPendingSMSStatus(context.Context, storage.Message) error {
	return nil
}
func (fakeWiFiCalling) ExecuteUSSD(context.Context, *mmodem.Modem, string, string) (string, error) {
	return "", nil
}
func (f fakeWiFiCalling) DialCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return f.voiceCall, f.dialErr
}
func (f fakeWiFiCalling) AnswerCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return f.answerCall, nil
}
func (f fakeWiFiCalling) RejectCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return f.rejectCall, nil
}
func (f fakeWiFiCalling) HangupCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return f.hangupCall, nil
}
func (f fakeWiFiCalling) HoldCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return f.holdCall, nil
}
func (f fakeWiFiCalling) ResumeCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return f.resumeCall, nil
}
func (f fakeWiFiCalling) SendCallDTMF(ctx context.Context, modem *mmodem.Modem, callID string, digits string) error {
	if f.sendDTMF != nil {
		return f.sendDTMF(callID, digits)
	}
	return f.dtmfErr
}
func (f fakeWiFiCalling) OpenCallMedia(context.Context, *mmodem.Modem, string) (wificalling.MediaSession, error) {
	if f.mediaErr != nil {
		return nil, f.mediaErr
	}
	return nil, wificalling.ErrUnavailable
}
func (f fakeWiFiCalling) SubscribeVoiceEvents(fn wificalling.VoiceEventFunc) func() {
	if f.subscribe != nil {
		return f.subscribe(fn)
	}
	return func() {}
}

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}
