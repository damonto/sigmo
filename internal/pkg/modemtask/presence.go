package modemtask

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/damonto/sigmo/internal/pkg/modem"
)

// Registry is the part of modem.Registry needed to follow modem presence.
type Registry interface {
	Modems(context.Context) (map[string]*modem.Modem, error)
	Subscribe(func(modem.ModemEvent) error) (func(), error)
}

type presenceSubscription struct {
	generation uint64
	cancel     context.CancelFunc
}

// Run starts one worker per present modem and cancels it when that modem
// generation disappears. Run returns after ctx is canceled and all workers exit.
func Run(ctx context.Context, registry Registry, start func(context.Context, *modem.Modem)) error {
	if registry == nil {
		return errors.New("modem registry is required")
	}
	if start == nil {
		return errors.New("modem start function is required")
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		stopping bool
		workers  = make(map[string]presenceSubscription)
	)
	stop := func(key string, generation uint64) {
		mu.Lock()
		worker, ok := workers[key]
		if ok && generation != 0 && worker.generation != generation {
			ok = false
		}
		if ok {
			delete(workers, key)
		}
		mu.Unlock()
		if ok {
			worker.cancel()
		}
	}
	startModem := func(m *modem.Modem, key string, generation uint64) {
		if m == nil || ctx.Err() != nil {
			return
		}
		if key == "" {
			key = m.Path()
		}
		if generation == 0 {
			generation = m.Generation()
		}

		modemCtx, cancel := context.WithCancel(ctx)
		mu.Lock()
		if stopping {
			mu.Unlock()
			cancel()
			return
		}
		old, exists := workers[key]
		if exists && old.generation == generation {
			mu.Unlock()
			cancel()
			return
		}
		workers[key] = presenceSubscription{generation: generation, cancel: cancel}
		wg.Add(1)
		mu.Unlock()
		if exists {
			old.cancel()
		}

		go func() {
			defer wg.Done()
			defer func() {
				mu.Lock()
				if current, ok := workers[key]; ok && current.generation == generation {
					delete(workers, key)
				}
				mu.Unlock()
			}()
			start(modemCtx, m)
		}()
	}
	stopAll := func() {
		mu.Lock()
		stopping = true
		cancels := make([]context.CancelFunc, 0, len(workers))
		for key, worker := range workers {
			delete(workers, key)
			cancels = append(cancels, worker.cancel)
		}
		mu.Unlock()
		for _, cancel := range cancels {
			cancel()
		}
		wg.Wait()
	}

	unsubscribe, err := registry.Subscribe(func(event modem.ModemEvent) error {
		switch event.Type {
		case modem.ModemEventAdded, modem.ModemEventChanged:
			startModem(event.Modem, event.Path, event.Generation)
			if event.Previous != nil && event.PreviousPath != "" && event.PreviousPath != event.Path {
				stop(event.PreviousPath, event.Previous.Generation())
			}
		case modem.ModemEventRemoved:
			stop(event.Path, event.Generation)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("subscribe modem presence: %w", err)
	}
	defer func() {
		unsubscribe()
		stopAll()
	}()

	present, err := registry.Modems(ctx)
	if err != nil {
		return fmt.Errorf("list present modems: %w", err)
	}
	for key, m := range present {
		startModem(m, key, m.Generation())
	}

	<-ctx.Done()
	return nil
}
