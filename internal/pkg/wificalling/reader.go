//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/vowifi-go/driver/at"
	"github.com/damonto/vowifi-go/driver/mbim"
	"github.com/damonto/vowifi-go/driver/qmi"
	usimreader "github.com/damonto/vowifi-go/usim/reader"
)

func openReader(ctx context.Context, modem *mmodem.Modem) (usimreader.Reader, error) {
	return openReaderWith(ctx, modem, openReaderCandidate)
}

type readerCandidate struct {
	portType mmodem.ModemPortType
	device   string
}

type readerOpener func(context.Context, readerCandidate, int) (usimreader.Reader, error)

func openReaderWith(ctx context.Context, modem *mmodem.Modem, open readerOpener) (usimreader.Reader, error) {
	slot := 1
	if modem.PrimarySimSlot > 0 {
		slot = int(modem.PrimarySimSlot)
	}
	candidates := readerCandidates(modem)
	if len(candidates) == 0 {
		return nil, errors.New("Wi-Fi Calling requires QMI, MBIM, or AT modem port")
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
	for _, portType := range []mmodem.ModemPortType{mmodem.ModemPortTypeQmi, mmodem.ModemPortTypeMbim, mmodem.ModemPortTypeAt} {
		for _, port := range modem.Ports {
			if port.PortType == portType {
				add(portType, port.Device)
			}
		}
	}
	return candidates
}

func supportedReaderPort(portType mmodem.ModemPortType) bool {
	return portType == mmodem.ModemPortTypeQmi || portType == mmodem.ModemPortTypeMbim || portType == mmodem.ModemPortTypeAt
}

func openReaderCandidate(ctx context.Context, candidate readerCandidate, slot int) (usimreader.Reader, error) {
	switch candidate.portType {
	case mmodem.ModemPortTypeQmi:
		return qmi.Open(ctx, candidate.device, slot)
	case mmodem.ModemPortTypeMbim:
		return mbim.Open(ctx, candidate.device, slot)
	case mmodem.ModemPortTypeAt:
		return at.New(candidate.device, 0)
	default:
		return nil, errors.New("reader port type is unsupported")
	}
}

func readerPortTypeName(portType mmodem.ModemPortType) string {
	switch portType {
	case mmodem.ModemPortTypeQmi:
		return "QMI"
	case mmodem.ModemPortTypeMbim:
		return "MBIM"
	case mmodem.ModemPortTypeAt:
		return "AT"
	default:
		return "unknown"
	}
}
