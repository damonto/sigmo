//go:build wifi_calling

package call

import (
	"maps"
	"slices"
	"sync"
)

type callEvents struct {
	mu          sync.Mutex
	subscribers map[uint64]chan Event
	nextSubID   uint64
}

func newCallEvents() *callEvents {
	return &callEvents{subscribers: make(map[uint64]chan Event)}
}

func (e *callEvents) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 8
	}
	ch := make(chan Event, buffer)
	e.mu.Lock()
	e.nextSubID++
	id := e.nextSubID
	e.subscribers[id] = ch
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.subscribers, id)
		e.mu.Unlock()
	}
}

func (e *callEvents) publish(event Event) {
	e.mu.Lock()
	subscribers := slices.Collect(maps.Values(e.subscribers))
	e.mu.Unlock()
	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}
