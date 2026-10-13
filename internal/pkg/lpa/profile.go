package lpa

import (
	"context"
	"errors"
	"fmt"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/modem"
)

// ActiveProfile reads only the snapshot's ICCID from each secure element. A SIM
// change or ambiguous enabled profiles returns nil rather than another SIM's data.
func (p *Pool) ActiveProfile(ctx context.Context, m *modem.Modem, identity modem.SIMIdentity) (*sgp22.ProfileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if identity.ICCID == "" || m.Snapshot().SIMIdentity != identity {
		return nil, nil
	}
	if p == nil {
		return nil, errPoolRequired
	}
	targets, err := p.targets(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("discover active SIM secure elements: %w", err)
	}
	var active *sgp22.ProfileInfo
	for _, target := range targets {
		if m.Snapshot().SIMIdentity != identity {
			return nil, nil
		}
		entry := p.entry(poolKey{modem: m, slot: target.slot, seID: target.sourceID})
		if entry == nil {
			return nil, fmt.Errorf("read active profile from %s: %w", target.se.ID, ErrSENotFound)
		}
		profiles, err := entry.profileInfo(ctx, identity)
		if err != nil {
			return nil, fmt.Errorf("read active profile from %s: %w", target.se.ID, err)
		}
		for _, profile := range profiles {
			if profile.ICCID.String() != identity.ICCID || profile.ProfileState != sgp22.ProfileEnabled {
				continue
			}
			if active != nil {
				return nil, nil
			}
			active = profile
		}
	}
	if m.Snapshot().SIMIdentity != identity {
		return nil, nil
	}
	return active, nil
}

// profileInfo releases each lease before another SE is read. Keeping defer in
// this function prevents leases from accumulating in ActiveProfile's loop.
func (entry *poolEntry) profileInfo(ctx context.Context, identity modem.SIMIdentity) (_ []*sgp22.ProfileInfo, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	iccid, err := sgp22.NewICCID(identity.ICCID)
	if err != nil {
		return nil, fmt.Errorf("parse active ICCID: %w", err)
	}
	client, err := lease(ctx, entry)
	if err != nil {
		return nil, fmt.Errorf("acquire client: %w", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("release client: %w", closeErr))
		}
	}()
	// Waiting for a lease can span a SIM switch or request cancellation.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if entry.modem.Snapshot().SIMIdentity != identity {
		return nil, nil
	}
	profiles, err := client.ListProfile(iccid, nil)
	if err != nil {
		return nil, fmt.Errorf("query ICCID: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return profiles, nil
}
