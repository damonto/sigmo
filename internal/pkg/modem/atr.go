package modem

import (
	"context"
	"fmt"
	"slices"

	uiccmbim "github.com/damonto/uicc-go/mbim"
	"github.com/damonto/uicc-go/qcom/uim"
)

type mbimATRReader interface {
	QueryUiccATR(ctx context.Context) ([]byte, error)
	Close() error
}

type qmiUIMOpener func(context.Context, string, uint8) (qmiUIMReader, error)
type mbimATROpener func(context.Context, ...uiccmbim.Option) (mbimATRReader, error)

type atrReader struct {
	openQMI  qmiUIMOpener
	openMBIM mbimATROpener
}

func newATRReader() atrReader {
	return atrReader{
		openQMI:  openQMIUIMReader,
		openMBIM: openUICCMBIMATRReader,
	}
}

func openUICCMBIMATRReader(ctx context.Context, opts ...uiccmbim.Option) (mbimATRReader, error) {
	return uiccmbim.Open(ctx, opts...)
}

var knownATRs = [][]byte{
	{0x3B, 0x9F, 0x96, 0x80, 0x3F, 0xC7, 0x82, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x15, 0x57, 0x65, 0x73, 0x74, 0x6B, 0x2E, 0x6D, 0x65, 0x63}, // eSTK.me
	{0x3B, 0x9F, 0x96, 0x80, 0x1F, 0xC7, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x15, 0x57, 0x65, 0x73, 0x74, 0x6B, 0x2E, 0x6D, 0x65, 0xC1},       // eSTK.me
	{0x3B, 0xBF, 0x93, 0x00, 0x80, 0x1F, 0xC6, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x13, 0x57, 0x65, 0x73, 0x74, 0x6B, 0x2E, 0x6D, 0x65, 0xE3}, // eSTK.me
	{0x3B, 0x9F, 0x96, 0x80, 0x1F, 0xC7, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x1B, 0x57, 0xAA, 0x86, 0x60, 0xF0, 0x02, 0x00, 0x02, 0x5C},       // ECP
	{0x3B, 0x9F, 0x96, 0x80, 0x1F, 0xC7, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x1B, 0x57, 0xAA, 0x86, 0x60, 0x16, 0x01, 0x00, 0x01, 0xBA},       // ECP
}

// SupportsEUICC detects eUICC support from the ATR cached on the SIM object.
// If ATR is unavailable, callers should fall back to trying ISD-R AIDs.
func SupportsEUICC(m *Modem) (bool, error) {
	if m == nil || m.Sim == nil || len(m.Sim.ATR) == 0 {
		return false, nil
	}
	return atrSupportsEUICC(m.Sim.ATR), nil
}

func (r atrReader) read(ctx context.Context, m *Modem) ([]byte, error) {
	switch m.PrimaryPortType() {
	case ModemPortTypeQmi:
		return r.readQMI(ctx, m)
	case ModemPortTypeMbim:
		return r.readMBIM(ctx, m)
	default:
		return nil, nil
	}
}

func (r atrReader) qmiOpener() qmiUIMOpener {
	if r.openQMI != nil {
		return r.openQMI
	}
	return openQMIUIMReader
}

func (r atrReader) mbimOpener() mbimATROpener {
	if r.openMBIM != nil {
		return r.openMBIM
	}
	return openUICCMBIMATRReader
}

func (r atrReader) readQMI(ctx context.Context, m *Modem) ([]byte, error) {
	requestedSlot, err := qmiSIMSlot(m)
	if err != nil {
		return nil, err
	}
	reader, err := r.qmiOpener()(ctx, m.PrimaryPort, requestedSlot)
	if err != nil {
		return nil, fmt.Errorf("open QMI UIM reader: %w", err)
	}
	defer closeQMIUIMReader(reader)

	status, err := reader.SlotStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("read QMI UIM slot status: %w", err)
	}
	slot, ok := qmiEUICCSlot(status, m.PrimarySimSlot, requestedSlot)
	if !ok {
		return nil, nil
	}
	m.Logger().Debug(
		"read QMI UICC ATR",
		"primarySlot", m.PrimarySimSlot,
		"activeSlot", status.ActiveSlot,
		"atr", formatATR(slot.ATR),
	)
	return slices.Clone(slot.ATR), nil
}

func qmiEUICCSlot(status uim.SlotStatus, primarySlot uint32, requestedSlot uint8) (uim.Slot, bool) {
	slot := requestedSlot
	if primarySlot == 0 && status.ActiveSlot != 0 {
		slot = status.ActiveSlot
	}
	index := int(slot) - 1
	if index < 0 || index >= len(status.Slots) {
		return uim.Slot{}, false
	}
	return status.Slots[index], true
}

func (r atrReader) readMBIM(ctx context.Context, m *Modem) ([]byte, error) {
	slot := int(m.PrimarySimSlot)
	if slot == 0 {
		slot = 1
	}
	reader, err := r.mbimOpener()(ctx, uiccmbim.WithProxy(m.PrimaryPort), uiccmbim.WithSlot(slot))
	if err != nil {
		return nil, fmt.Errorf("open MBIM reader: %w", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			m.Logger().Debug("close MBIM reader", "error", err)
		}
	}()

	atr, err := reader.QueryUiccATR(ctx)
	if err != nil {
		return nil, fmt.Errorf("query MBIM UICC ATR: %w", err)
	}
	m.Logger().Debug("read MBIM UICC ATR", "slot", slot, "atr", formatATR(atr))
	return slices.Clone(atr), nil
}

func formatATR(atr []byte) string {
	return fmt.Sprintf("% X", atr)
}

func atrSupportsEUICC(atr []byte) bool {
	tb, ok := atrT15GlobalTB(atr)
	if ok && tb&0x82 == 0x82 {
		return true
	}
	return knownATR(atr)
}

func knownATR(atr []byte) bool {
	return slices.ContainsFunc(knownATRs, func(known []byte) bool {
		return slices.Equal(known, atr)
	})
}

// ETSI TS 102 221 declares eUICC support in TB after a TD byte announces T=15.
func atrT15GlobalTB(atr []byte) (byte, bool) {
	if len(atr) < 2 {
		return 0, false
	}
	if atr[0] != 0x3B && atr[0] != 0x3F {
		return 0, false
	}

	y := atr[1] >> 4
	historicalLen := int(atr[1] & 0x0F)
	index := 2
	protocol := byte(0)
	group := 1
	var tb byte
	found := false
	needsChecksum := false

	for {
		if y&0x1 != 0 {
			if index >= len(atr) {
				return 0, false
			}
			index++
		}
		if y&0x2 != 0 {
			if index >= len(atr) {
				return 0, false
			}
			if protocol == 0x0F && group > 2 {
				tb = atr[index]
				found = true
			}
			index++
		}
		if y&0x4 != 0 {
			if index >= len(atr) {
				return 0, false
			}
			index++
		}
		if y&0x8 == 0 {
			break
		}
		if index >= len(atr) {
			return 0, false
		}
		td := atr[index]
		index++
		y = td >> 4
		protocol = td & 0x0F
		group++
		if protocol != 0 {
			needsChecksum = true
		}
	}
	end := index + historicalLen
	if needsChecksum {
		if end >= len(atr) || end+1 != len(atr) {
			return 0, false
		}
		var checksum byte
		for _, b := range atr[1:] {
			checksum ^= b
		}
		if checksum != 0 {
			return 0, false
		}
		return tb, found
	}
	if end != len(atr) {
		return 0, false
	}
	return tb, found
}
