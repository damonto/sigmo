package esim

import (
	"context"
	"fmt"
	"strings"

	elpa "github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"

	ilpa "github.com/damonto/sigmo/internal/pkg/lpa"
)

// Profiles reads installed profiles after resolving a modem ID.
func (s *Service) Profiles(ctx context.Context, modemID string) (*Inventory, error) {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return s.profile.List(ctx, device)
}

// DiscoverProfiles returns profiles advertised for a modem's secure element.
func (s *Service) DiscoverProfiles(ctx context.Context, modemID string, seID string) ([]Discovery, error) {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return s.provisioning.Discovery(ctx, device, seID)
}

// DownloadProfile provisions a profile using caller-owned interaction callbacks.
func (s *Service) DownloadProfile(ctx context.Context, modemID string, seID string, activationCode *elpa.ActivationCode, opts *elpa.DownloadOptions) error {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	return s.provisioning.Download(ctx, device, seID, activationCode, opts)
}

// ActivationCode builds an activation code using the selected modem's IMEI.
func (s *Service) ActivationCode(ctx context.Context, modemID string, smdp string, matchingID string) (*elpa.ActivationCode, error) {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	var address ilpa.SMDPAddress
	if err := address.UnmarshalText([]byte(smdp)); err != nil {
		return nil, err
	}
	imei, err := device.ThreeGPP().IMEI(ctx)
	if err != nil {
		return nil, fmt.Errorf("read modem IMEI: %w", err)
	}
	return &elpa.ActivationCode{
		SMDP:       address.URL(),
		MatchingID: strings.TrimSpace(matchingID),
		IMEI:       imei,
	}, nil
}

// ActivateProfile enables a profile through the same lifecycle used by transfers.
func (s *Service) ActivateProfile(ctx context.Context, modemID, seID, rawICCID string) error {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	iccid, err := sgp22.NewICCID(strings.TrimSpace(rawICCID))
	if err != nil {
		return fmt.Errorf("parse ICCID: %w", err)
	}
	return s.EnableProfile(ctx, device, seID, iccid)
}

// RenameProfile updates a nickname after resolving the modem and ICCID.
func (s *Service) RenameProfile(ctx context.Context, modemID string, seID string, rawICCID string, nickname string) error {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	iccid, err := sgp22.NewICCID(strings.TrimSpace(rawICCID))
	if err != nil {
		return fmt.Errorf("parse ICCID: %w", err)
	}
	return s.profile.UpdateNickname(ctx, device, seID, iccid, nickname)
}

// RemoveProfile uses the shared deletion lifecycle after resolving modem and profile IDs.
func (s *Service) RemoveProfile(ctx context.Context, modemID string, seID string, rawICCID string) error {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	iccid, err := sgp22.NewICCID(strings.TrimSpace(rawICCID))
	if err != nil {
		return fmt.Errorf("parse ICCID: %w", err)
	}
	return s.DeleteProfile(ctx, device, seID, iccid)
}
