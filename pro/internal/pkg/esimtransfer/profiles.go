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

func (s *Service) profileResponses(ctx context.Context, currentSettings *settings.Settings, req ProfilesRequest) ([]ProfileResponse, error) {
	candidates, err := s.profileCandidates(ctx, currentSettings, req)
	if err != nil {
		return nil, err
	}
	response := make([]ProfileResponse, 0, len(candidates))
	for _, candidate := range candidates {
		response = append(response, candidate.response)
	}
	return response, nil
}

func (s *Service) profileCandidates(ctx context.Context, currentSettings *settings.Settings, req ProfilesRequest) ([]profileCandidate, error) {
	switch req.SourceType {
	case SourceModem:
		return s.modemProfileCandidates(ctx, currentSettings, req)
	case SourceCCID:
		return s.ccidProfileCandidates(ctx, currentSettings, req)
	default:
		return nil, ErrSourceUnsupported
	}
}

func (s *Service) modemProfileCandidates(ctx context.Context, currentSettings *settings.Settings, req ProfilesRequest) ([]profileCandidate, error) {
	modem, err := s.registry.Find(ctx, req.SourceID)
	if err != nil {
		return nil, err
	}
	profiles, err := sourceModemProfiles(modem, currentSettings)
	if err == nil {
		return esimCandidates(profiles), nil
	}
	if !errors.Is(err, ilpa.ErrNoSupportedAID) {
		return nil, err
	}
	return s.physicalProfileCandidates(ctx, currentSettings, req)
}

func (s *Service) ccidProfileCandidates(ctx context.Context, currentSettings *settings.Settings, req ProfilesRequest) ([]profileCandidate, error) {
	profiles, err := sourceCCIDProfiles(currentSettings, req)
	if err == nil {
		return esimCandidates(profiles), nil
	}
	if !errors.Is(err, ilpa.ErrNoSupportedAID) {
		return nil, err
	}
	return s.physicalProfileCandidates(ctx, currentSettings, req)
}

func (s *Service) physicalProfileCandidates(ctx context.Context, currentSettings *settings.Settings, req ProfilesRequest) ([]profileCandidate, error) {
	source, err := s.openSource(ctx, currentSettings, Start{
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
	candidate := physicalCandidate(*identity)
	return []profileCandidate{candidate}, nil
}

func sourceCCIDProfiles(currentSettings *settings.Settings, req ProfilesRequest) ([]*sgp22.ProfileInfo, error) {
	reader, err := openCCIDLPAReader(req.SourceID)
	if err != nil {
		return nil, fmt.Errorf("open CCID reader: %w", err)
	}
	client, err := ilpa.NewWithChannel(ilpa.ChannelConfig{
		LockKey:  sourceLockKey(SourceCCID, req.SourceID),
		Channel:  reader,
		Settings: currentSettings,
		Logger: sourceLogger(Start{
			SourceType: SourceCCID,
			SourceID:   req.SourceID,
			SourceIMEI: req.SourceIMEI,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("create source LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			client.Logger().Warn("close source LPA client", "error", cerr)
		}
	}()
	profiles, err := client.ListProfile(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list source profiles: %w", err)
	}
	return profiles, nil
}

func esimCandidates(profiles []*sgp22.ProfileInfo) []profileCandidate {
	response := make([]profileCandidate, 0, len(profiles))
	for _, profile := range profiles {
		response = append(response, esimCandidate(profile))
	}
	return response
}

func esimCandidate(profile *sgp22.ProfileInfo) profileCandidate {
	mcc := profile.ProfileOwner.MCC()
	mnc := profile.ProfileOwner.MNC()
	gid1 := strings.ToUpper(hex.EncodeToString(profile.ProfileOwner.GID1))
	enabled := profile.ProfileState == sgp22.ProfileEnabled
	supported, reason := support(ts43.Identity{MCC: mcc, MNC: mnc, GID1: gid1}, ts43.SIMTypeESIM, "eSIM")
	carrierName := carrierName(mcc + mnc)
	return profileCandidate{
		response: ProfileResponse{
			ID:                  profile.ICCID.String(),
			Type:                ProfileESIM,
			Name:                profileDisplayName(profile),
			ServiceProviderName: profile.ServiceProviderName,
			ICCID:               profile.ICCID.String(),
			Icon:                profileIconDataURL(profile.Icon),
			RegionCode:          profileRegion(profile),
			Enabled:             enabled,
			Supported:           supported,
			UnsupportedReason:   reason,
			CarrierName:         carrierName,
		},
	}
}

func physicalCandidate(identity ts43.Identity) profileCandidate {
	carrierInfo := carrier.Lookup(identity.MCC + identity.MNC)
	supported, reason := support(identity, ts43.SIMTypePSIM, "pSIM")
	name := carrierInfo.Name
	if name == "" || name == "Unknown" {
		name = identity.MCC + identity.MNC
	}
	carrierName := name
	if carrierInfo.Name == "Unknown" {
		carrierName = ""
	}
	return profileCandidate{
		response: ProfileResponse{
			ID:                "physical:" + identity.ICCID,
			Type:              ProfilePhysical,
			Name:              name,
			ICCID:             identity.ICCID,
			RegionCode:        carrierInfo.Region,
			Enabled:           true,
			Supported:         supported,
			UnsupportedReason: reason,
			CarrierName:       carrierName,
		},
	}
}

func support(identity ts43.Identity, sourceSIMType ts43.SIMType, label string) (bool, string) {
	if _, err := ts43.DiscoverEntitlement(identity, sourceSIMType); err != nil {
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

func findCandidate(profiles []profileCandidate, id string) (profileCandidate, bool) {
	for _, profile := range profiles {
		if profile.response.ID != id {
			continue
		}
		return profile, true
	}
	return profileCandidate{}, false
}
