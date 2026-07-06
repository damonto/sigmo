package stk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	uiccat "github.com/damonto/uicc-go/at"
	"github.com/damonto/uicc-go/usim"
	usimcard "github.com/damonto/uicc-go/usim/card"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	mdevice "github.com/damonto/sigmo/internal/pkg/modem/device"
)

func OpenCard(ctx context.Context, modem *mmodem.Modem) (Card, error) {
	if modem == nil {
		return Card{}, errModemRequired
	}
	card, err := openDeviceCard(ctx, modem)
	if err == nil {
		return card, nil
	}
	if !errors.Is(err, mdevice.ErrUnsupported) {
		return Card{}, err
	}
	return openATCard(ctx, modem)
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

func openATCard(ctx context.Context, modem *mmodem.Modem) (Card, error) {
	port, err := modem.Port(mmodem.ModemPortTypeAt)
	if err != nil {
		return Card{}, fmt.Errorf("find AT port: %w", err)
	}
	tx, err := uiccat.Open(port.Device, 0)
	if err != nil {
		return Card{}, fmt.Errorf("open AT reader: %w", err)
	}
	reader, err := usim.NewReader(tx)
	if err != nil {
		_ = tx.Close()
		return Card{}, err
	}
	return openUSIMCard(ctx, reader, modem.Logger())
}

func openUSIMCard(ctx context.Context, reader usimcard.Reader, logger *slog.Logger) (Card, error) {
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
		Close: card.Close,
	}, nil
}
