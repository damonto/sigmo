//go:build ims

package call

import (
	"context"
	"errors"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type callRecords struct {
	store  *storage.Store
	events *callEvents
}

func (r *callRecords) List(ctx context.Context, modem *mmodem.Modem, query string) ([]storage.Call, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return nil, err
	}
	return r.store.ListCalls(ctx, profileID, modem.EquipmentIdentifier, 50, query)
}

func (r *callRecords) callForAction(ctx context.Context, modem *mmodem.Modem, callID string) (storage.Call, error) {
	call, err := r.store.GetCall(ctx, callID)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.Call{}, ErrCallNotFound
	}
	if err != nil {
		return storage.Call{}, err
	}
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return storage.Call{}, err
	}
	if call.ProfileID != profileID || call.ModemID != modem.EquipmentIdentifier {
		return storage.Call{}, ErrCallNotFound
	}
	return call, nil
}

func (r *callRecords) saveAndPublish(ctx context.Context, call storage.Call) (storage.Call, error) {
	call, publish, err := r.store.SaveCallPreservingTerminal(ctx, call)
	if err != nil {
		return storage.Call{}, err
	}
	if publish {
		r.events.publish(Event{Call: call})
	}
	return call, nil
}

func (r *callRecords) deleteCall(ctx context.Context, call storage.Call) error {
	if !isTerminalCallState(call.State) {
		return ErrCallRecordActive
	}
	if err := r.store.DeleteCall(ctx, call.ProfileID, call.ModemID, call.ID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return ErrCallNotFound
		}
		return err
	}
	return nil
}
