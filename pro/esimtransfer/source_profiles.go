//go:build esim_transfer

package esimtransfer

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/ts43-go"

	"github.com/damonto/sigmo/internal/pkg/carrier"
	ilpa "github.com/damonto/sigmo/internal/pkg/lpa"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

func (s *transferRunner) sourceProfileOptions(ctx context.Context, currentSettings *settings.Settings, req ProfilesRequest) ([]ProfileResponse, error) {
	switch req.SourceType {
	case SourceModem:
		return s.modemProfileOptions(ctx, currentSettings, req)
	case SourceCCID:
		return s.ccidProfileOptions(ctx, currentSettings, req)
	default:
		return nil, ErrSourceUnsupported
	}
}

func (s *transferRunner) modemProfileOptions(ctx context.Context, currentSettings *settings.Settings, req ProfilesRequest) ([]ProfileResponse, error) {
	modem, err := s.registry.Find(ctx, req.SourceID)
	if err != nil {
		return nil, err
	}
	profiles, err := s.sourceModemProfiles(ctx, modem, currentSettings)
	if err == nil {
		return modemESIMOptions(profiles), nil
	}
	if !errors.Is(err, ilpa.ErrNoSupportedAID) {
		return nil, err
	}
	return s.physicalProfileOptions(ctx, currentSettings, req)
}

func (s *transferRunner) ccidProfileOptions(ctx context.Context, currentSettings *settings.Settings, req ProfilesRequest) ([]ProfileResponse, error) {
	profiles, err := sourceCCIDProfiles(ctx, currentSettings, req)
	if err == nil {
		return esimOptions(profiles, ""), nil
	}
	if !errors.Is(err, ilpa.ErrNoSupportedAID) {
		return nil, err
	}
	return s.physicalProfileOptions(ctx, currentSettings, req)
}

func (s *transferRunner) physicalProfileOptions(ctx context.Context, currentSettings *settings.Settings, req ProfilesRequest) ([]ProfileResponse, error) {
	source, err := s.openSource(ctx, currentSettings, startRequest{
		SourceType: req.SourceType,
		SourceID:   req.SourceID,
		SourceIMEI: req.SourceIMEI,
	})
	if err != nil {
		return nil, err
	}
	defer source.Close()

	identity, err := source.channel.Identity(ctx)
	if err != nil {
		return nil, fmt.Errorf("read source SIM identity: %w", err)
	}
	option := physicalOption(*identity)
	return []ProfileResponse{option}, nil
}

func sourceCCIDProfiles(ctx context.Context, currentSettings *settings.Settings, req ProfilesRequest) ([]*sgp22.ProfileInfo, error) {
	start := startRequest{
		SourceType: SourceCCID,
		SourceID:   req.SourceID,
		SourceIMEI: req.SourceIMEI,
	}
	sourceLPA, err := newCCIDLPAClient(ctx, currentSettings, start)
	if err != nil {
		return nil, fmt.Errorf("create source LPA client: %w", err)
	}
	defer func() {
		if cerr := sourceLPA.Close(); cerr != nil {
			sourceLPA.Logger().Warn("close source LPA client", "error", cerr)
		}
	}()
	profiles, err := sourceLPA.ListProfile(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list source profiles: %w", err)
	}
	return profiles, nil
}

func modemESIMOptions(profiles []sourceModemProfile) []ProfileResponse {
	response := make([]ProfileResponse, 0, len(profiles))
	for _, profile := range profiles {
		response = append(response, esimOption(profile.profile, profile.seID))
	}
	return response
}

func esimOptions(profiles []*sgp22.ProfileInfo, seID string) []ProfileResponse {
	response := make([]ProfileResponse, 0, len(profiles))
	for _, profile := range profiles {
		response = append(response, esimOption(profile, seID))
	}
	return response
}

func esimOption(profile *sgp22.ProfileInfo, seID string) ProfileResponse {
	mcc := profile.ProfileOwner.MCC()
	mnc := profile.ProfileOwner.MNC()
	gid1 := strings.ToUpper(hex.EncodeToString(profile.ProfileOwner.GID1))
	enabled := profile.ProfileState == sgp22.ProfileEnabled
	supported, reason := transferSupport(ts43.Identity{MCC: mcc, MNC: mnc, GID1: gid1}, ts43.SIMTypeESIM, "eSIM")
	carrierName := carrierName(mcc + mnc)
	id := profile.ICCID.String()
	if seID != "" {
		id = seID + ":" + id
	}
	return ProfileResponse{
		ID:                  id,
		SEID:                seID,
		Type:                ProfileESIM,
		Name:                profileDisplayName(profile),
		ServiceProviderName: profile.ServiceProviderName,
		ICCID:               profile.ICCID.String(),
		ISDPAID:             profile.ISDPAID.String(),
		Icon:                profileIconDataURL(profile.Icon),
		ProfileName:         profile.ProfileName,
		ProfileNickname:     profile.ProfileNickname,
		ProfileStateName:    profile.ProfileState.String(),
		ProfileClass:        profile.ProfileClass.String(),
		ProfileOwner:        profileOwnerResponse(profile.ProfileOwner),
		RegionCode:          profileRegion(profile),
		Enabled:             enabled,
		Supported:           supported,
		UnsupportedReason:   reason,
		CarrierName:         carrierName,
	}
}

func physicalOption(identity ts43.Identity) ProfileResponse {
	carrierInfo := carrier.Lookup(identity.MCC + identity.MNC)
	supported, reason := transferSupport(identity, ts43.SIMTypePSIM, "pSIM")
	name := carrierInfo.Name
	if name == "" || name == "Unknown" {
		name = identity.MCC + identity.MNC
	}
	carrierName := name
	if carrierInfo.Name == "Unknown" {
		carrierName = ""
	}
	return ProfileResponse{
		ID:                "physical:" + identity.ICCID,
		Type:              ProfilePhysical,
		Name:              name,
		ICCID:             identity.ICCID,
		RegionCode:        carrierInfo.Region,
		Enabled:           true,
		Supported:         supported,
		UnsupportedReason: reason,
		CarrierName:       carrierName,
	}
}

func transferSupport(identity ts43.Identity, simType ts43.SIMType, label string) (bool, string) {
	if _, err := ts43.DiscoverEntitlement(identity, simType); err != nil {
		if errors.Is(err, ts43.ErrEntitlementAmbiguous) {
			return false, "carrier entitlement config is ambiguous"
		}
		if errors.Is(err, ts43.ErrEntitlementUnsupported) || errors.Is(err, ts43.ErrEntitlementNotFound) {
			return false, "carrier does not support " + label + " transfer"
		}
		return false, "carrier does not support " + label + " transfer"
	}
	return true, ""
}

func carrierName(mccmnc string) string {
	carrierInfo := carrier.Lookup(mccmnc)
	if carrierInfo.Name == "Unknown" {
		return ""
	}
	return carrierInfo.Name
}

func findSourceProfileOption(profiles []ProfileResponse, id string) (ProfileResponse, bool) {
	for _, profile := range profiles {
		if profile.ID != id {
			continue
		}
		return profile, true
	}
	return ProfileResponse{}, false
}
