package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/damonto/sigmo/internal/pkg/carrier"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modemtask"
	"github.com/damonto/sigmo/internal/pkg/phonenumber"
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
			target := current.SIMIdentity()
			if target.ICCID == "" {
				return nil
			}
			err := current.RestoreStoredNumber(target, func(snapshot modem.ModemSnapshot) (string, error) {
				region := carrier.Lookup(snapshot.SIM.OperatorIdentifier).Region
				number := phonenumber.NormalizeOwnNumber(snapshot.CardNumber, region)
				if !strings.HasPrefix(number, "+") {
					// Keep an unqualified reading available as a live fallback.
					number = ""
				}
				if err := store.RecordSIMProfile(ctx, storage.SIMProfile{
					ProfileID: target.ICCID, ModemID: current.EquipmentIdentifier, Number: number,
				}); err != nil {
					return "", err
				}
				numbers, err := store.SIMProfileNumbers(ctx, []string{target.ICCID})
				return phonenumber.NormalizeOwnNumber(numbers[target.ICCID], region), err
			})
			if errors.Is(err, modem.ErrSIMActivationChanged) {
				return nil
			}
			return err
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
