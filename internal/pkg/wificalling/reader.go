//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/uicc-go/at"
	"github.com/damonto/uicc-go/qualcomm/qmi"
	"github.com/damonto/uicc-go/qualcomm/uim"
	"github.com/damonto/uicc-go/usim"
	usimcard "github.com/damonto/uicc-go/usim/card"
)

func openReader(ctx context.Context, modem *mmodem.Modem) (usimcard.Reader, error) {
	return openReaderWith(ctx, modem, openReaderCandidate)
}

type readerCandidate struct {
	portType mmodem.ModemPortType
	device   string
}

type readerOpener func(context.Context, readerCandidate, int) (usimcard.Reader, error)

func openReaderWith(ctx context.Context, modem *mmodem.Modem, open readerOpener) (usimcard.Reader, error) {
	slot := 1
	if modem.PrimarySimSlot > 0 {
		slot = int(modem.PrimarySimSlot)
	}
	candidates := readerCandidates(modem)
	if len(candidates) == 0 {
		return nil, errors.New("Wi-Fi Calling requires QMI or AT modem port")
	}
	var result error
	for _, candidate := range candidates {
		reader, err := open(ctx, candidate, slot)
		if err == nil {
			return reader, nil
		}
		result = errors.Join(result, fmt.Errorf("open %s reader on %s: %w", readerPortTypeName(candidate.portType), candidate.device, err))
	}
	return nil, result
}

func readerCandidates(modem *mmodem.Modem) []readerCandidate {
	if modem == nil {
		return nil
	}
	var candidates []readerCandidate
	add := func(portType mmodem.ModemPortType, device string) {
		device = strings.TrimSpace(device)
		if device == "" || !supportedReaderPort(portType) {
			return
		}
		for _, candidate := range candidates {
			if candidate.portType == portType && candidate.device == device {
				return
			}
		}
		candidates = append(candidates, readerCandidate{portType: portType, device: device})
	}
	add(modem.PrimaryPortType(), modem.PrimaryPort)
	for _, portType := range []mmodem.ModemPortType{mmodem.ModemPortTypeQmi, mmodem.ModemPortTypeAt} {
		for _, port := range modem.Ports {
			if port.PortType == portType {
				add(portType, port.Device)
			}
		}
	}
	return candidates
}

func supportedReaderPort(portType mmodem.ModemPortType) bool {
	return portType == mmodem.ModemPortTypeQmi || portType == mmodem.ModemPortTypeAt
}

func openReaderCandidate(ctx context.Context, candidate readerCandidate, slot int) (usimcard.Reader, error) {
	switch candidate.portType {
	case mmodem.ModemPortTypeQmi:
		if slot < 1 || slot > 5 {
			return nil, fmt.Errorf("slot %d is out of range", slot)
		}
		transport, err := qmi.Open(ctx, qmi.WithProxy(candidate.device))
		if err != nil {
			return nil, err
		}
		reader, err := uim.New(ctx, transport, uim.WithSlot(uint8(slot)))
		if err != nil {
			return nil, errors.Join(err, transport.Close())
		}
		if err := reader.ActivateSlot(ctx); err != nil {
			return nil, errors.Join(err, reader.Close())
		}
		adapter, err := usim.NewQualcomm(reader)
		if err != nil {
			return nil, errors.Join(err, reader.Close())
		}
		return adapter, nil
	case mmodem.ModemPortTypeAt:
		tx, err := at.Open(ctx, candidate.device, 0)
		if err != nil {
			return nil, err
		}
		reader, err := usim.NewReader(tx)
		if err != nil {
			return nil, errors.Join(err, tx.Close())
		}
		return reader, nil
	default:
		return nil, errors.New("reader port type is unsupported")
	}
}

func readerPortTypeName(portType mmodem.ModemPortType) string {
	switch portType {
	case mmodem.ModemPortTypeQmi:
		return "QMI"
	case mmodem.ModemPortTypeAt:
		return "AT"
	default:
		return "unknown"
	}
}
