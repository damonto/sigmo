package simapp

import (
	"context"
	"errors"

	stkpkg "github.com/damonto/wwan-go/sim/stk"
)

type rootMenuSelector interface {
	SelectMenu(context.Context, byte, bool) (stkpkg.EnvelopeResponse, error)
}

type envelopeRootSelector struct {
	sender envelopeSender
}

type envelopeSender interface {
	SendEnvelope(context.Context, stkpkg.Envelope) (stkpkg.EnvelopeResponse, error)
}

func (s envelopeRootSelector) SelectMenu(ctx context.Context, item byte, helpRequested bool) (stkpkg.EnvelopeResponse, error) {
	return s.sender.SendEnvelope(ctx, stkpkg.MenuSelection(item, helpRequested))
}

func (s *wsSession) rootSelectionLoop(ctx context.Context, selector rootMenuSelector, ready <-chan struct{}) {
	if ready != nil {
		select {
		case <-ready:
		case <-ctx.Done():
			return
		case <-s.disconnectCh:
			return
		}
	}

	for {
		select {
		case msg := <-s.rootCh:
			if err := s.selectRootMenu(ctx, selector, msg); err != nil {
				s.sendIfConnected(wsServerMessage{Type: wsTypeError, Message: err.Error()})
			}
		case <-ctx.Done():
			return
		case <-s.disconnectCh:
			return
		}
	}
}

func (s *wsSession) selectRootMenu(ctx context.Context, selector rootMenuSelector, msg wsClientMessage) error {
	item, ok := byteFromItemID(msg.ItemID)
	if !ok {
		return errors.New("menu item id is out of range")
	}
	if !s.rootMenuHasItem(item) {
		return errors.New("menu item is not available")
	}
	_, err := selector.SelectMenu(ctx, item, msg.HelpRequested)
	return err
}
