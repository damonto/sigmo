package call

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	imsvoice "github.com/damonto/ims-go/ims/voice"
	pims "github.com/damonto/sigmo/internal/app/ims"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/pion/webrtc/v4"
)

func TestMediaReconnectReplacesOnlyTheOwnersRTPReader(t *testing.T) {
	store := testStore(t)
	call := storage.Call{ID: "active", ModemID: "modem", ProfileID: "sim", Route: RouteWiFiCalling,
		Direction: DirectionIncoming, Number: "10086", State: StateActive, OwnerClientID: "ios"}
	if err := store.SaveCall(t.Context(), call); err != nil {
		t.Fatal(err)
	}
	packets := &clientTestMedia{started: make(chan struct{}, 4)}
	voice := clientMediaVoice{media: packets}
	media, err := NewMedia(t.Context(), New(store, voice))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := media.Shutdown(t.Context()); err != nil {
			t.Error(err)
		}
	})
	// An unavailable local credential endpoint exercises direct ICE without
	// contacting TURN, a carrier, or any external notification service.
	ice := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ice.Close()
	media.ice = webRTCICEProvider{client: ice.Client(), endpoint: ice.URL}
	modem := &mmodem.Modem{EquipmentIdentifier: call.ModemID, SIM: &mmodem.SIM{Identifier: call.ProfileID}}
	request := ClientMedia{CallID: call.ID, ClientID: "ios"}
	first, err := media.OpenClientSession(t.Context(), modem, request)
	if err != nil {
		t.Fatal(err)
	}
	connectClientMedia(t, first)
	waitClientMedia(t, packets.started)
	// Storage normalizes IDs, so aliases must replace the same call's RTP reader.
	second, err := media.OpenClientSession(t.Context(), modem, ClientMedia{CallID: " " + call.ID + " ", ClientID: request.ClientID})
	if err != nil {
		t.Fatal(err)
	}
	if packets.readers.Load() != 0 || first.bridge.pc.ConnectionState() != webrtc.PeerConnectionStateClosed {
		t.Fatal("replacement returned before the old RTP reader closed")
	}
	connectClientMedia(t, second)
	waitClientMedia(t, packets.started)
	first.Close(t.Context())
	media.bridgeMu.Lock()
	current, count := media.clients[call.ID], len(media.bridges)
	media.bridgeMu.Unlock()
	if current != second.bridge || count != 1 {
		t.Fatal("late close removed the replacement bridge")
	}
	if _, err := media.OpenClientSession(t.Context(), modem, ClientMedia{CallID: call.ID, ClientID: "web"}); !errors.Is(err, ErrCallInUse) {
		t.Fatalf("competing media: %v", err)
	}
	// Two recovery attempts from this installation can overlap, but only one
	// resulting bridge remains live after both requests complete.
	var wg sync.WaitGroup
	var recovered [2]*WebRTCSession
	for index := range recovered {
		wg.Go(func() {
			var err error
			recovered[index], err = media.OpenClientSession(t.Context(), modem, request)
			if err != nil {
				t.Errorf("reconnect: %v", err)
			}
		})
	}
	wg.Wait()
	media.bridgeMu.Lock()
	count = len(media.bridges)
	media.bridgeMu.Unlock()
	if count != 1 || packets.overlap.Load() {
		t.Fatalf("bridges = %d, overlapping readers = %t", count, packets.overlap.Load())
	}
}

func connectClientMedia(t *testing.T, session *WebRTCSession) {
	t.Helper()
	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := peer.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := peer.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	waitClientMedia(t, gathered)
	gathered = webrtc.GatheringCompletePromise(session.bridge.pc)
	if _, err := session.Answer(t.Context(), WebRTCSessionDescription{Type: "offer", SDP: peer.LocalDescription().SDP}); err != nil {
		t.Fatal(err)
	}
	waitClientMedia(t, gathered)
	if err := peer.SetRemoteDescription(*session.bridge.pc.LocalDescription()); err != nil {
		t.Fatal(err)
	}
}

func waitClientMedia(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("local media did not become ready")
	}
}

type clientMediaVoice struct {
	fakeIMSVoice
	media *clientTestMedia
}

func (v clientMediaVoice) OpenCallMedia(context.Context, *mmodem.Modem, string) (pims.MediaSession, error) {
	return v.media, nil
}

type clientTestMedia struct {
	started chan struct{}
	readers atomic.Int32
	overlap atomic.Bool
}

func (m *clientTestMedia) Media() imsvoice.NegotiatedMedia {
	return negotiatedMedia(imsvoice.CodecPCMU, 8000)
}

func (m *clientTestMedia) ReadPacket(ctx context.Context) ([]byte, error) {
	if m.readers.Add(1) != 1 {
		m.overlap.Store(true)
	}
	defer m.readers.Add(-1)
	m.started <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *clientTestMedia) WritePacket(context.Context, []byte) error { return nil }
