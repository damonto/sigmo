//go:build ims

package call

import (
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestCallEventsFilterBeforeEnqueue(t *testing.T) {
	state := Event{Call: storage.Call{ID: "call-1", ModemID: "modem-1", State: StateActive}}
	dtmf := Event{Call: state.Call, DTMF: &DTMF{Digit: "5", At: time.Now()}}
	otherState := Event{Call: storage.Call{ID: "call-2", ModemID: "modem-2", State: StateActive}}
	otherDTMF := Event{Call: otherState.Call, DTMF: dtmf.DTMF}
	tests := []struct {
		name    string
		config  SubscriptionConfig
		ignored []Event
		want    Event
	}{
		{
			name:    "notification ignores DTMF from all modems",
			config:  SubscriptionConfig{Buffer: 1},
			ignored: []Event{dtmf, otherDTMF},
			want:    otherState,
		},
		{
			name:    "modem state ignores other modems and DTMF",
			config:  SubscriptionConfig{ModemID: "modem-1", Buffer: 1},
			ignored: []Event{dtmf, otherState, otherDTMF},
			want:    state,
		},
		{
			name:    "modem DTMF ignores other modems and state",
			config:  SubscriptionConfig{Kind: EventKindDTMF, ModemID: "modem-1", Buffer: 1},
			ignored: []Event{state, otherState, otherDTMF},
			want:    dtmf,
		},
		{
			name:    "DTMF subscription can include all modems",
			config:  SubscriptionConfig{Kind: EventKindDTMF, Buffer: 1},
			ignored: []Event{state, otherState},
			want:    otherDTMF,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := New(nil, nil)
			events, unsubscribe := service.Subscribe(tt.config)
			defer unsubscribe()
			// A paused consumer must not lose a relevant event to unrelated traffic.
			for range 32 {
				for _, event := range tt.ignored {
					service.events.publish(event)
				}
			}
			service.events.publish(tt.want)
			select {
			case got := <-events:
				if got != tt.want {
					t.Fatalf("event = %+v, want %+v", got, tt.want)
				}
			default:
				t.Fatal("relevant event was dropped")
			}
		})
	}
}

func TestCallEventsKeepStateWhenDTMFBufferIsFull(t *testing.T) {
	service := New(nil, nil)
	states, unsubscribeState := service.Subscribe(SubscriptionConfig{ModemID: "modem-1", Buffer: 1})
	defer unsubscribeState()
	digits, unsubscribeDTMF := service.Subscribe(SubscriptionConfig{Kind: EventKindDTMF, ModemID: "modem-1", Buffer: 1})
	defer unsubscribeDTMF()
	fastDigits, unsubscribeFast := service.Subscribe(SubscriptionConfig{Kind: EventKindDTMF, ModemID: "modem-1", Buffer: 1})
	defer unsubscribeFast()

	call := storage.Call{ID: "call-1", ModemID: "modem-1", State: StateActive}
	first := &DTMF{Digit: "0", At: time.Now()}
	for i := range 32 {
		dtmf := first
		if i > 0 {
			dtmf = &DTMF{Digit: "5", At: first.At.Add(time.Duration(i) * time.Second)}
		}
		service.events.publish(Event{Call: call, DTMF: dtmf})
		select {
		case got := <-fastDigits:
			if got.DTMF == nil || *got.DTMF != *dtmf {
				t.Fatalf("fast subscriber DTMF = %+v, want %+v", got.DTMF, dtmf)
			}
		default:
			t.Fatal("slow subscriber blocked delivery to fast subscriber")
		}
	}
	call.State = StateEnded
	service.events.publish(Event{Call: call})

	select {
	case got := <-states:
		if got.Call != call || got.DTMF != nil {
			t.Fatalf("state event = %+v, want ended call", got)
		}
	default:
		t.Fatal("DTMF traffic displaced the ended state")
	}
	select {
	case got := <-digits:
		if got.DTMF == nil || *got.DTMF != *first {
			t.Fatalf("slow subscriber DTMF = %+v, want first queued digit", got.DTMF)
		}
	default:
		t.Fatal("first DTMF event was lost")
	}
}
