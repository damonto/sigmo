package mdn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/damonto/sigmo/internal/pkg/mdn"
	"github.com/damonto/sigmo/internal/pkg/modem"
	usim "github.com/damonto/wwan-go/sim"
)

type checkedTransport struct {
	check func() error
	base  http.RoundTripper
}

func (t checkedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.check(); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

func queryNumber(ctx context.Context, m *modem.Modem, target modem.SIMIdentity, catalog *mdn.Configs, check func() error) (string, error) {
	if catalog.Empty() {
		return "", mdn.ErrUnsupported
	}
	if sim := m.Snapshot().SIM; sim != nil && (len(sim.OperatorIdentifier) == 5 || len(sim.OperatorIdentifier) == 6) {
		plmn := mdn.Identity{MCC: sim.OperatorIdentifier[:3], MNC: sim.OperatorIdentifier[3:], MNCLength: 2}
		if !catalog.HasPLMN(plmn) {
			return "", mdn.ErrUnsupported
		}
	}
	card, err := openCard(ctx, m, target, check)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := card.Close(); err != nil {
			m.Logger().Debug("close MDN SIM reader", "error", err)
		}
	}()
	identity := mdn.Identity{MCC: card.MCC(), MNC: card.MNC(), MNCLength: card.MNCLength(), IMSI: card.IMSI(),
		ICCID: card.ICCID(), IMEI: m.EquipmentIdentifier, SPN: card.SPN(), GID1: card.GID1(), GID2: card.GID2()}
	profile, err := catalog.Select(identity)
	if err != nil {
		return "", err
	}
	client := mdn.NewClient(&http.Client{Transport: checkedTransport{check: check, base: http.DefaultTransport}})
	number, err := client.Lookup(ctx, profile, identity, func(ctx context.Context, rand, autn []byte) (usim.AKAResult, error) {
		release, err := m.ReserveSIMSlot(ctx)
		if err != nil {
			return usim.AKAResult{}, err
		}
		defer release()
		if err := check(); err != nil {
			return usim.AKAResult{}, err
		}
		return card.AKA(ctx, rand, autn)
	})
	if err != nil {
		return "", fmt.Errorf("carrier %q (PLMN %s%s): %w", profile.CarrierName, identity.MCC, identity.MNC, err)
	}
	return number, nil
}

func openCard(ctx context.Context, m *modem.Modem, target modem.SIMIdentity, check func() error) (*usim.Card, error) {
	release, err := m.ReserveSIMSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := check(); err != nil {
		return nil, err
	}
	device, err := modem.OpenDevice(m)
	if err != nil {
		return nil, err
	}
	reader, err := device.USIM(ctx)
	if err != nil {
		return nil, err
	}
	// The library's card-load debug log includes subscriber identities. Surface
	// errors through the service without logging those fields during a lookup.
	card, err := usim.New(ctx, reader, slog.New(slog.DiscardHandler))
	if err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	if card.ICCID() != target.ICCID || check() != nil {
		return nil, errors.Join(context.Canceled, card.Close())
	}
	return card, nil
}
