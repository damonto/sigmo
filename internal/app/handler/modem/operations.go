package modem

import (
	"context"
	"errors"
	"time"
)

func (h *Handler) ListModems(ctx context.Context) ([]*ModemResponse, error) {
	return h.catalog.List(ctx)
}

func (h *Handler) Modem(ctx context.Context, id string) (*ModemResponse, error) {
	device, err := h.registry.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	return h.catalog.Get(ctx, device)
}

func (h *Handler) SwitchSIM(ctx context.Context, id string, identifier string) error {
	device, err := h.registry.Find(ctx, id)
	if err != nil {
		return err
	}
	slotIndex, err := h.simSlot.targetIndex(ctx, device, identifier)
	if err != nil {
		return err
	}
	switchCtx, cancel := context.WithTimeout(ctx, switchSimSlotTimeout)
	defer cancel()
	if err := h.internet.Restore(switchCtx, device); err != nil {
		return err
	}
	if err := h.simSlot.Switch(switchCtx, device, slotIndex); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errSwitchSimSlotTimeout
		}
		return err
	}
	return nil
}

func (h *Handler) UpdateNumber(ctx context.Context, id string, number string) error {
	device, err := h.registry.Find(ctx, id)
	if err != nil {
		return err
	}
	updateCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return h.msisdn.Update(updateCtx, device, number)
}
