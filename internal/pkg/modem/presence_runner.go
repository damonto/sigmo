package modem

import (
	"context"
	"errors"
	"sync"
)

type presenceTask struct {
	registry *Registry
	start    func(context.Context, *Modem)
}

type presenceSubscription struct {
	generation uint64
	cancel     context.CancelFunc
}

func newPresenceTask(registry *Registry, start func(context.Context, *Modem)) *presenceTask {
	return &presenceTask{registry: registry, start: start}
}

func RunPresenceTask(ctx context.Context, registry *Registry, start func(context.Context, *Modem)) error {
	return newPresenceTask(registry, start).Run(ctx)
}

func (t *presenceTask) Run(ctx context.Context) error {
	if t.registry == nil {
		return errors.New("modem registry is required")
	}
	if t.start == nil {
		return errors.New("modem start function is required")
	}
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		stopping bool
		subs     = make(map[string]presenceSubscription)
	)
	stop := func(key string, generation uint64) {
		mu.Lock()
		sub, ok := subs[key]
		if ok && generation != 0 && sub.generation != generation {
			ok = false
		}
		if ok {
			delete(subs, key)
		}
		mu.Unlock()
		if ok {
			sub.cancel()
		}
	}
	startModem := func(m *Modem) {
		if m == nil || ctx.Err() != nil {
			return
		}
		key := m.Path()
		modemCtx, cancel := context.WithCancel(ctx)
		mu.Lock()
		if stopping {
			mu.Unlock()
			cancel()
			return
		}
		old, exists := subs[key]
		if exists && old.generation == m.Generation() {
			mu.Unlock()
			cancel()
			return
		}
		subs[key] = presenceSubscription{generation: m.Generation(), cancel: cancel}
		wg.Add(1)
		mu.Unlock()
		if exists {
			old.cancel()
		}
		go func() {
			defer wg.Done()
			defer func() {
				mu.Lock()
				if current, ok := subs[key]; ok && current.generation == m.Generation() {
					delete(subs, key)
				}
				mu.Unlock()
			}()
			t.start(modemCtx, m)
		}()
	}
	stopAll := func() {
		mu.Lock()
		stopping = true
		all := make([]context.CancelFunc, 0, len(subs))
		for key, sub := range subs {
			delete(subs, key)
			all = append(all, sub.cancel)
		}
		mu.Unlock()
		for _, cancel := range all {
			cancel()
		}
		wg.Wait()
	}
	unsubscribe, err := t.registry.Subscribe(func(event ModemEvent) error {
		switch event.Type {
		case ModemEventAdded, ModemEventChanged:
			startModem(event.Modem)
			if event.Previous != nil && event.Previous.Path() != event.Path {
				stop(event.Previous.Path(), event.Previous.Generation())
			}
		case ModemEventRemoved:
			stop(event.Path, event.Generation)
		}
		return nil
	})
	if err != nil {
		return err
	}
	defer func() { unsubscribe(); stopAll() }()
	modems, err := t.registry.Modems(ctx)
	if err != nil {
		return err
	}
	for _, m := range modems {
		startModem(m)
	}
	<-ctx.Done()
	return nil
}
