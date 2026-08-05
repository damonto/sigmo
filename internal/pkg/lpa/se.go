package lpa

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/damonto/euicc-go/driver"
	"github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

const (
	SEIDDefault = "default"
	SEID0       = "se0"
	SEID1       = "se1"
)

var (
	ErrSERequired     = errors.New("eUICC SE is required")
	ErrSENotFound     = errors.New("eUICC SE not found")
	errSIMSlotChanged = errors.New("SIM slot changed during LPA operation")
)

type SE struct {
	ID    string
	Label string
	AID   []byte
}

var DefaultSE = SE{
	ID:    SEIDDefault,
	Label: "eUICC",
}

type seChannelOpener func(context.Context, *modem.Modem) (driver.SmartCardChannel, error)

// DiscoverSEs returns the eUICC secure elements available through m.
func DiscoverSEs(ctx context.Context, m *modem.Modem) ([]SE, error) {
	return discoverSEs(ctx, m, createChannel)
}

func discoverSEs(ctx context.Context, m *modem.Modem, openChannel seChannelOpener) ([]SE, error) {
	if m == nil {
		return nil, errors.New("modem is required")
	}
	releaseSIMSlot, err := m.ReserveSIMSlot(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve SIM slot for eUICC SE detection: %w", err)
	}
	defer releaseSIMSlot()

	slot, err := modem.ActiveSIMSlot(m)
	if err != nil {
		return nil, err
	}
	return discoverSEsAtSlot(ctx, m, slot, openChannel)
}

func discoverSEsForSlot(ctx context.Context, m *modem.Modem, slot uint8) ([]SE, error) {
	if m == nil {
		return nil, errors.New("modem is required")
	}
	releaseSIMSlot, err := m.ReserveSIMSlot(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve SIM slot for eUICC SE detection: %w", err)
	}
	defer releaseSIMSlot()

	activeSlot, err := modem.ActiveSIMSlot(m)
	if err != nil {
		return nil, err
	}
	if activeSlot != slot {
		return nil, errSIMSlotChanged
	}
	return discoverSEsAtSlot(ctx, m, slot, func(ctx context.Context, m *modem.Modem) (driver.SmartCardChannel, error) {
		return createChannelForSlot(ctx, m, slot)
	})
}

func discoverSEsAtSlot(ctx context.Context, m *modem.Modem, slot uint8, openChannel seChannelOpener) ([]SE, error) {
	if m == nil {
		return nil, errors.New("modem is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot := m.Snapshot()
	sim := snapshot.SIM
	sameSlot := sim != nil && (sim.Slot == uint32(slot) || (sim.Slot == 0 && (snapshot.PrimarySIMSlot == uint32(slot) || (snapshot.PrimarySIMSlot == 0 && slot == 1))))
	if sameSlot && len(sim.ATR) > 0 && !isESTKmeATR(sim.ATR) {
		return []SE{DefaultSE}, nil
	}
	if (!sameSlot || len(sim.ATR) == 0) && m.PrimaryPortType() != wwanmodem.PortQMI && m.PrimaryPortType() != wwanmodem.PortMBIM {
		return []SE{DefaultSE}, nil
	}

	if err := gmu.LockContext(ctx, lpaLockKey(m, slot)); err != nil {
		return nil, err
	}
	defer gmu.Unlock(lpaLockKey(m, slot))

	ch, err := openChannel(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("create channel for eUICC SE detection: %w", err)
	}
	defer func() {
		if err := ch.Disconnect(); err != nil {
			m.Logger().Debug("disconnect eUICC SE detection channel", "error", err)
		}
	}()

	ses, ok, err := estkmeSEs(ch, m.Logger())
	if err != nil {
		return nil, fmt.Errorf("discover ESTKme secure elements: %w", err)
	}
	if ok {
		return ses, nil
	}
	return []SE{DefaultSE}, nil
}

// ResolveSE resolves an eUICC secure element.
func ResolveSE(ctx context.Context, m *modem.Modem, id string) (SE, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SE{}, ErrSERequired
	}
	ses, err := DiscoverSEs(ctx, m)
	if err != nil {
		return SE{}, err
	}
	for _, se := range ses {
		if se.ID == id {
			se.AID = slices.Clone(se.AID)
			return se, nil
		}
	}
	return SE{}, fmt.Errorf("%w: %s", ErrSENotFound, id)
}
