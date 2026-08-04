package lpa

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/damonto/euicc-go/driver"
	"github.com/damonto/sigmo/internal/pkg/modem"
)

const (
	SEIDDefault = "default"
	SEID0       = "se0"
	SEID1       = "se1"
)

var (
	ErrSERequired = errors.New("eUICC SE is required")
	ErrSENotFound = errors.New("eUICC SE not found")
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sim := m.Snapshot().SIM
	if sim == nil {
		return []SE{DefaultSE}, nil
	}
	if !isESTKmeATR(sim.ATR) {
		return []SE{DefaultSE}, nil
	}

	if err := gmu.LockContext(ctx, m.EquipmentIdentifier); err != nil {
		return nil, err
	}
	defer gmu.Unlock(m.EquipmentIdentifier)

	ch, err := openChannel(ctx, m)
	if err != nil {
		m.Logger().Debug("create channel for eUICC SE detection", "error", err)
		return []SE{DefaultSE}, nil
	}

	ses, ok := estkmeSEs(ch, m.Logger())
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
