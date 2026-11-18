package lpa

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/modem"
)

// cardReadCache is accessed under the pool entry's lease.
type cardReadCache struct {
	identity modem.SIMIdentity
	eid      *string
	profiles []*sgp22.ProfileInfo
	info     *Info
}

func (c *cardReadCache) invalidateProfileData() {
	if c == nil {
		return
	}
	// The card may have changed even if the operation returns a transport error.
	c.profiles = nil
	c.info = nil
}

func (c *cardReadCache) invalidateInfo() {
	if c != nil {
		c.info = nil
	}
}

// ProfileInventory is the EID and installed profiles of one secure element.
type ProfileInventory struct {
	SE       SE
	EID      string
	Profiles []*sgp22.ProfileInfo
}

// ProfileInventories returns the installed profiles for the active SIM's secure elements.
func (p *Pool) ProfileInventories(ctx context.Context, m *modem.Modem) ([]ProfileInventory, error) {
	return readCards(ctx, p, m, func(se SE, client *clientView, cache *cardReadCache) (ProfileInventory, error) {
		eid, err := cache.readEID(client)
		if err != nil {
			return ProfileInventory{}, err
		}
		// ListProfile returns a non-nil slice even when the card has no profiles.
		if cache.profiles == nil {
			profiles, err := client.ListProfile(nil, nil)
			if err != nil {
				return ProfileInventory{}, fmt.Errorf("read profiles: %w", err)
			}
			cache.profiles = profiles
		}
		return ProfileInventory{SE: se, EID: eid, Profiles: cloneProfiles(cache.profiles)}, nil
	})
}

// SecureElementInfo is the card information for one secure element.
type SecureElementInfo struct {
	SE   SE
	Info Info
}

// EUICCInfo returns the card information for the active SIM's secure elements.
func (p *Pool) EUICCInfo(ctx context.Context, m *modem.Modem) ([]SecureElementInfo, error) {
	return readCards(ctx, p, m, func(se SE, client *clientView, cache *cardReadCache) (SecureElementInfo, error) {
		if cache.info == nil {
			eid, err := cache.readEID(client)
			if err != nil {
				return SecureElementInfo{}, err
			}
			info, err := client.readInfo(eid)
			if err != nil {
				return SecureElementInfo{}, err
			}
			cache.info = info
		}
		info := *cache.info
		info.Certificates = slices.Clone(info.Certificates)
		return SecureElementInfo{SE: se, Info: info}, nil
	})
}

func (c *cardReadCache) readEID(client *clientView) (string, error) {
	if c.eid != nil {
		return *c.eid, nil
	}
	rawEID, err := client.EID()
	if err != nil {
		return "", fmt.Errorf("read EID: %w", err)
	}
	eid := hex.EncodeToString(rawEID)
	c.eid = &eid
	return eid, nil
}

func readCards[T any](ctx context.Context, p *Pool, m *modem.Modem, read func(SE, *clientView, *cardReadCache) (T, error)) ([]T, error) {
	if p == nil {
		return nil, errPoolRequired
	}
	if m == nil {
		return nil, errors.New("modem is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.SIMReloadPending() {
		return nil, ErrModemRetired
	}
	identity := m.SIMIdentity()
	targets, err := p.targets(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("discover eUICC secure elements: %w", err)
	}
	results := make([]T, 0, len(targets))
	for _, target := range targets {
		entry := p.entry(poolKey{modem: m, slot: target.slot, seID: target.sourceID})
		if entry == nil {
			return nil, fmt.Errorf("read secure element %s: %w", target.se.ID, ErrSENotFound)
		}
		item, err := readCard(ctx, entry, identity, target.se, read)
		if err != nil {
			return nil, fmt.Errorf("read secure element %s: %w", target.se.ID, err)
		}
		results = append(results, item)
	}
	if m.SIMReloadPending() {
		return nil, ErrModemRetired
	}
	if m.SIMIdentity() != identity {
		return nil, errSIMSlotChanged
	}
	return results, nil
}

func readCard[T any](ctx context.Context, entry *poolEntry, identity modem.SIMIdentity, se SE, read func(SE, *clientView, *cardReadCache) (T, error)) (result T, err error) {
	client, err := lease(ctx, entry)
	if err != nil {
		return result, fmt.Errorf("acquire LPA client: %w", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("release LPA client: %w", closeErr))
		}
	}()
	if err := entry.checkCardIdentity(ctx, identity); err != nil {
		return result, err
	}
	// Stage reads until the current activation has been verified again.
	cache := entry.cache
	if cache.identity != identity {
		cache = cardReadCache{identity: identity}
	}
	item, err := read(se, client.clientView, &cache)
	if checkErr := entry.checkCardIdentity(ctx, identity); checkErr != nil {
		return result, errors.Join(err, checkErr)
	}
	entry.cache = cache
	return item, err
}

func (entry *poolEntry) checkCardIdentity(ctx context.Context, identity modem.SIMIdentity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-entry.done:
		return entry.err
	case <-entry.modem.Done():
		return ErrModemRetired
	default:
	}
	if entry.modem.SIMReloadPending() {
		return ErrModemRetired
	}
	if entry.modem.SIMIdentity() != identity || max(identity.Slot, 1) != uint32(entry.key.slot) {
		return errSIMSlotChanged
	}
	return nil
}

func cloneProfiles(profiles []*sgp22.ProfileInfo) []*sgp22.ProfileInfo {
	result := slices.Clone(profiles)
	for i, profile := range profiles {
		cloned := *profile
		cloned.ICCID = slices.Clone(profile.ICCID)
		cloned.ISDPAID = slices.Clone(profile.ISDPAID)
		cloned.Icon = slices.Clone(profile.Icon)
		cloned.ProfileOwner.PLMN = slices.Clone(profile.ProfileOwner.PLMN)
		cloned.ProfileOwner.GID1 = slices.Clone(profile.ProfileOwner.GID1)
		cloned.ProfileOwner.GID2 = slices.Clone(profile.ProfileOwner.GID2)
		cloned.NotificationConfigurationInfo = slices.Clone(profile.NotificationConfigurationInfo)
		for j, configuration := range profile.NotificationConfigurationInfo {
			clonedConfig := *configuration
			clonedConfig.ProfileManagementOperations = slices.Clone(configuration.ProfileManagementOperations)
			cloned.NotificationConfigurationInfo[j] = &clonedConfig
		}
		if profile.SMDPProprietaryData != nil {
			cloned.SMDPProprietaryData = profile.SMDPProprietaryData.Clone()
		}
		if profile.ServiceSpecificData != nil {
			cloned.ServiceSpecificData = profile.ServiceSpecificData.Clone()
		}
		result[i] = &cloned
	}
	return result
}
