package call

import (
	"errors"
	"sync"
	"testing"
	"time"

	pims "github.com/damonto/sigmo/internal/app/ims"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestClientsCompeteForOneAnswer(t *testing.T) {
	store := testStore(t)
	call := storage.Call{ID: "incoming", ModemID: "modem", ProfileID: "sim", Route: RouteWiFiCalling, Direction: DirectionIncoming,
		Number: "10086", State: StateRinging, StartedAt: time.Now(), UpdatedAt: time.Now()}
	if err := store.SaveCall(t.Context(), call); err != nil {
		t.Fatal(err)
	}
	voice := fakeIMSVoice{answerCall: pims.VoiceCall{ID: call.ID, ModemID: call.ModemID, ProfileID: call.ProfileID, Route: call.Route,
		Direction: call.Direction, Number: call.Number, State: StateActive, StartedAt: call.StartedAt, AnsweredAt: time.Now(), UpdatedAt: time.Now()}}
	service := New(store, voice)
	modem := &mmodem.Modem{EquipmentIdentifier: call.ModemID, SIM: &mmodem.SIM{Identifier: call.ProfileID}}
	var wg sync.WaitGroup
	var results [2]error
	for index, owner := range []string{"web", "ios"} {
		wg.Go(func() {
			_, results[index] = service.UpdateForClient(t.Context(), modem, ClientUpdate{CallID: call.ID, ClientID: owner, Update: UpdateRequest{State: StateActive}})
		})
	}
	wg.Wait()
	successes, conflicts := 0, 0
	for _, err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrCallInUse) {
			conflicts++
		} else {
			t.Fatalf("answer: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("answer results: %v", results)
	}
	saved, err := store.Call(t.Context(), call.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The winner can reconcile/retry without another carrier answer.
	if _, err := service.UpdateForClient(t.Context(), modem, ClientUpdate{CallID: call.ID, ClientID: saved.OwnerClientID, Update: UpdateRequest{State: StateActive}}); err != nil {
		t.Fatal(err)
	}
	if saved.State != StateActive || saved.CallUUID == "" {
		t.Fatalf("claimed snapshot: %+v", saved)
	}
}

func TestClientActionsCannotAffectAnotherOwner(t *testing.T) {
	store := testStore(t)
	call := storage.Call{ID: "active", ModemID: "modem", ProfileID: "sim", Route: RouteWiFiCalling, Direction: DirectionOutgoing,
		Number: "10086", State: StateActive, OwnerClientID: "ios", StartedAt: time.Now(), UpdatedAt: time.Now()}
	if err := store.SaveCall(t.Context(), call); err != nil {
		t.Fatal(err)
	}
	service := New(store, fakeIMSVoice{})
	modem := &mmodem.Modem{EquipmentIdentifier: call.ModemID, SIM: &mmodem.SIM{Identifier: call.ProfileID}}
	for _, update := range []UpdateRequest{{State: StateEnded}, {State: StateActive}, {Hold: HoldLocal}} {
		if _, err := service.UpdateForClient(t.Context(), modem, ClientUpdate{CallID: call.ID, ClientID: "web", Update: update}); !errors.Is(err, ErrCallInUse) {
			t.Fatalf("competing update %+v: %v", update, err)
		}
	}
	if err := service.SendDTMFForClient(t.Context(), modem, ClientDTMF{CallID: call.ID, ClientID: "web", Digits: "1"}); !errors.Is(err, ErrCallInUse) {
		t.Fatalf("competing DTMF: %v", err)
	}
	media := &Media{calls: service}
	if _, err := media.OpenClientSession(t.Context(), modem, ClientMedia{CallID: call.ID, ClientID: "web"}); !errors.Is(err, ErrCallInUse) {
		t.Fatalf("competing media: %v", err)
	}
	if _, err := service.DialForClient(t.Context(), modem, DialRequest{To: "10086", ClientID: "ios", ExpectedProfileID: "old-sim"}); !errors.Is(err, ErrCallProfileChanged) {
		t.Fatalf("dial replacement SIM: %v", err)
	}
}

func TestOpeningMediaCannotClaimRingingCall(t *testing.T) {
	store := testStore(t)
	call := storage.Call{ID: "ringing", ModemID: "modem", ProfileID: "sim", Route: RouteWiFiCalling, Direction: DirectionIncoming,
		Number: "10086", State: StateRinging, StartedAt: time.Now(), UpdatedAt: time.Now()}
	if err := store.SaveCall(t.Context(), call); err != nil {
		t.Fatal(err)
	}
	media := &Media{calls: New(store, fakeIMSVoice{})}
	modem := &mmodem.Modem{EquipmentIdentifier: call.ModemID, SIM: &mmodem.SIM{Identifier: call.ProfileID}}
	if _, err := media.OpenClientSession(t.Context(), modem, ClientMedia{CallID: call.ID, ClientID: "viewer"}); !errors.Is(err, ErrMediaUnavailable) {
		t.Fatalf("open ringing media: %v", err)
	}
	saved, err := store.Call(t.Context(), call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.OwnerClientID != "" {
		t.Fatalf("a viewer claimed the ringing call: %q", saved.OwnerClientID)
	}
}
