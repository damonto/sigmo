package stk

import (
	"context"
	"fmt"
	"log/slog"

	usim "github.com/damonto/wwan-go/sim"
	usimcard "github.com/damonto/wwan-go/sim/card"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func OpenCard(ctx context.Context, modem *mmodem.Modem) (Card, error) {
	if modem == nil {
		return Card{}, errModemRequired
	}
	return openDeviceCard(ctx, modem)
}

func openDeviceCard(ctx context.Context, modem *mmodem.Modem) (Card, error) {
	device, err := mmodem.OpenDevice(modem)
	if err != nil {
		return Card{}, fmt.Errorf("open device: %w", err)
	}
	reader, err := device.USIMWithCAT(ctx, terminalCATProfile())
	if err != nil {
		return Card{}, fmt.Errorf("open device USIM reader: %w", err)
	}
	return openUSIMCard(ctx, reader, modem.Logger())
}

func openUSIMCard(ctx context.Context, reader usimcard.Reader, logger *slog.Logger) (Card, error) {
	var ready <-chan struct{}
	if source, ok := reader.(interface{ CATReady() <-chan struct{} }); ok {
		ready = source.CATReady()
	}
	card, err := usim.New(ctx, reader, logger)
	if err != nil {
		_ = reader.Close()
		return Card{}, fmt.Errorf("open USIM card: %w", err)
	}
	stk, err := card.STK()
	if err != nil {
		_ = card.Close()
		return Card{}, err
	}
	return Card{
		ICCID: card.ICCID(),
		STK:   stk,
		Ready: ready,
		Close: card.Close,
	}, nil
}
