//go:build ims

package call

import "sync"

// EventKind selects the events delivered to a subscription.
type EventKind uint8

const (
	// EventKindCall selects call state updates.
	EventKindCall EventKind = iota
	// EventKindDTMF selects remote key detections.
	EventKindDTMF
)

// SubscriptionConfig limits which events can occupy a subscriber's buffer.
type SubscriptionConfig struct {
	// Kind defaults to call state updates.
	Kind EventKind
	// ModemID limits delivery to one modem; empty matches all modems.
	ModemID string
	// Buffer defaults to 8 when non-positive. Full buffers drop new events.
	Buffer int
}

type callSubscriber struct {
	config SubscriptionConfig
	events chan Event
}

type callEvents struct {
	mu          sync.Mutex
	subscribers map[uint64]callSubscriber
	nextSubID   uint64
}

func newCallEvents() *callEvents {
	return &callEvents{subscribers: make(map[uint64]callSubscriber)}
}

func (e *callEvents) Subscribe(config SubscriptionConfig) (<-chan Event, func()) {
	if config.Buffer <= 0 {
		config.Buffer = 8
	}
	ch := make(chan Event, config.Buffer)
	e.mu.Lock()
	e.nextSubID++
	id := e.nextSubID
	e.subscribers[id] = callSubscriber{config: config, events: ch}
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.subscribers, id)
		e.mu.Unlock()
	}
}

func (e *callEvents) publish(event Event) {
	kind := EventKindCall
	if event.DTMF != nil {
		kind = EventKindDTMF
	}
	// Delivery is non-blocking, so holding the lock also makes unsubscribe final.
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, sub := range e.subscribers {
		if sub.config.Kind != kind || (sub.config.ModemID != "" && sub.config.ModemID != event.Call.ModemID) {
			continue
		}
		select {
		case sub.events <- event:
		default:
		}
	}
}
