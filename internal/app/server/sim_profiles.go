package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modemtask"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func subscribeSIMProfiles(ctx context.Context, registry modemtask.Registry, store *storage.Store) (func(), error) {
	var mu sync.Mutex
	record := func(current *modem.Modem) error {
		if current == nil {
			return nil
		}
		// Read after taking the lock so startup and delayed events cannot overwrite
		// a newer observation. Retired modem generations must never update history.
		mu.Lock()
		defer mu.Unlock()
		present, err := registry.Modems(ctx)
		if err != nil {
			return fmt.Errorf("list modems for SIM profiles: %w", err)
		}
		for _, active := range present {
			if active != current {
				continue
			}
			snapshot := current.Snapshot()
			return store.RecordSIMProfile(ctx, storage.SIMProfile{
				ProfileID: snapshot.SIMIdentity.ICCID,
				ModemID:   current.EquipmentIdentifier,
				Number:    snapshot.Number,
			})
		}
		return nil
	}
	unsubscribe, err := registry.Subscribe(ctx, func(event modem.ModemEvent) error {
		switch event.Type {
		case modem.ModemEventAdded, modem.ModemEventChanged, modem.ModemEventSIMChanged, modem.ModemEventNumberChanged:
			return record(event.Modem)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe SIM profile updates: %w", err)
	}
	present, err := registry.Modems(ctx)
	if err != nil {
		unsubscribe()
		return nil, fmt.Errorf("list initial SIM profiles: %w", err)
	}
	for _, current := range present {
		if err := record(current); err != nil {
			unsubscribe()
			return nil, fmt.Errorf("record initial SIM profile: %w", err)
		}
	}
	return unsubscribe, nil
}
