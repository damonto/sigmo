package modem

import (
	"context"
	"errors"
	"fmt"
	"slices"

	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
)

var knownATRs = [][]byte{
	{0x3B, 0x9F, 0x96, 0x80, 0x3F, 0xC7, 0x82, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x15, 0x57, 0x65, 0x73, 0x74, 0x6B, 0x2E, 0x6D, 0x65, 0x63}, // eSTK.me
	{0x3B, 0x9F, 0x96, 0x80, 0x1F, 0xC7, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x15, 0x57, 0x65, 0x73, 0x74, 0x6B, 0x2E, 0x6D, 0x65, 0xC1},       // eSTK.me
	{0x3B, 0xBF, 0x93, 0x00, 0x80, 0x1F, 0xC6, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x13, 0x57, 0x65, 0x73, 0x74, 0x6B, 0x2E, 0x6D, 0x65, 0xE3}, // eSTK.me
	{0x3B, 0x9F, 0x96, 0x80, 0x1F, 0xC7, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x1B, 0x57, 0xAA, 0x86, 0x60, 0x16, 0x01, 0x00, 0x01, 0xBA},       // ECP
	{0x3B, 0x9F, 0x96, 0x80, 0x1F, 0xC7, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x1B, 0x57, 0xAA, 0x86, 0x60, 0xF0, 0x02, 0x00, 0x02, 0x5C},       // ECP
	{0x3B, 0x9F, 0x96, 0x80, 0x1F, 0xC7, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x1B, 0x57, 0xAA, 0x86, 0x60, 0xF0, 0x02, 0x00, 0x03, 0x5D},       // ECP
}

type deviceATRReader interface {
	ATR(context.Context) ([]byte, error)
}

type deviceATROpener func(*Modem) (deviceATRReader, error)

// SupportsEUICC detects eUICC support from the ATR cached on the SIM object.
func SupportsEUICC(m *Modem) (bool, error) {
	if m == nil || m.Sim == nil || len(m.Sim.ATR) == 0 {
		return false, nil
	}
	return atrSupportsEUICC(m.Sim.ATR), nil
}

func readDeviceATR(ctx context.Context, m *Modem, open deviceATROpener) ([]byte, error) {
	if open == nil {
		open = func(m *Modem) (deviceATRReader, error) {
			return OpenDevice(m)
		}
	}
	device, err := open(m)
	if errors.Is(err, wwan.ErrUnsupported) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	atr, err := device.ATR(ctx)
	if err != nil {
		return nil, err
	}
	m.Logger().Debug("read device ATR", "primarySlot", m.PrimarySimSlot, "atr", formatATR(atr))
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
