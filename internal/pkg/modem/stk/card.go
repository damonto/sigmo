package stk

import (
	"context"
	"fmt"
	"log/slog"

	uiccat "github.com/damonto/uicc-go/at"
	uiccmbim "github.com/damonto/uicc-go/mbim"
	"github.com/damonto/uicc-go/qcom/qmi"
	"github.com/damonto/uicc-go/qcom/uim"
	"github.com/damonto/uicc-go/usim"
	usimcard "github.com/damonto/uicc-go/usim/card"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const maxSlot = 5

func OpenCard(ctx context.Context, modem *mmodem.Modem) (Card, error) {
	if modem == nil {
		return Card{}, errModemRequired
	}
	switch modem.PrimaryPortType() {
	case mmodem.ModemPortTypeQmi:
		return openQMICard(ctx, modem)
	case mmodem.ModemPortTypeMbim:
		return openMBIMCard(ctx, modem)
	default:
		return openATCard(ctx, modem)
	}
}

func openQMICard(ctx context.Context, modem *mmodem.Modem) (Card, error) {
	slot, err := cardSlot(modem)
	if err != nil {
		return Card{}, err
	}
	transport, err := qmi.Open(ctx, qmi.WithProxy(modem.PrimaryPort))
	if err != nil {
		return Card{}, fmt.Errorf("open QMI transport: %w", err)
	}
	reader, err := uim.New(ctx, transport, uim.WithSlot(slot))
	if err != nil {
		_ = transport.Close()
		return Card{}, fmt.Errorf("open QMI UIM reader: %w", err)
	}
	if err := ensureCATReady(ctx, modem, uim.NewCAT(reader)); err != nil {
		_ = reader.Close()
		return Card{}, err
	}

	q, err := usim.NewQCOM(reader)
	if err != nil {
		_ = reader.Close()
		return Card{}, err
	}
	return openUSIMCard(ctx, q, modem.Logger())
}

func openMBIMCard(ctx context.Context, modem *mmodem.Modem) (Card, error) {
	slot, err := cardSlot(modem)
	if err != nil {
		return Card{}, err
	}
	reader, err := uiccmbim.Open(ctx, uiccmbim.WithProxy(modem.PrimaryPort), uiccmbim.WithSlot(int(slot)))
	if err != nil {
		return Card{}, fmt.Errorf("open MBIM reader: %w", err)
	}
	m, err := usim.NewMBIM(reader)
	if err != nil {
		_ = reader.Close()
		return Card{}, err
	}
	return openUSIMCard(ctx, m, modem.Logger())
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

func cardSlot(modem *mmodem.Modem) (uint8, error) {
	if modem == nil {
		return 0, errModemRequired
	}
	if modem.PrimarySimSlot == 0 {
		return 1, nil
	}
	if modem.PrimarySimSlot > maxSlot {
		return 0, fmt.Errorf("SIM slot %d is out of range", modem.PrimarySimSlot)
	}
	return uint8(modem.PrimarySimSlot), nil
}
