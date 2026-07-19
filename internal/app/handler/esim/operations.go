package esim

import (
	"context"
	"fmt"
	"strings"
	"time"

	elpa "github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"

	ilpa "github.com/damonto/sigmo/internal/pkg/lpa"
)

func (h *Handler) Profiles(ctx context.Context, modemID string) (*ProfilesResponse, error) {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return h.profile.List(ctx, device)
}

func (h *Handler) DiscoverProfiles(ctx context.Context, modemID string, seID string) ([]DiscoverResponse, error) {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return h.provisioning.Discovery(ctx, device, seID)
}

func (h *Handler) DownloadProfile(ctx context.Context, modemID string, seID string, activationCode *elpa.ActivationCode, opts *elpa.DownloadOptions) error {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	return h.provisioning.Download(ctx, device, seID, activationCode, opts)
}

func (h *Handler) ActivationCode(ctx context.Context, modemID string, smdp string, matchingID string) (*elpa.ActivationCode, error) {
	device, err := h.registry.Find(ctx, modemID)
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

func (h *Handler) ActivateProfile(ctx context.Context, modemID string, seID string, rawICCID string) error {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	iccid, err := sgp22.NewICCID(strings.TrimSpace(rawICCID))
	if err != nil {
		return fmt.Errorf("parse ICCID: %w", err)
	}
	session, err := h.lifecycle.PrepareEnable(device, seID, iccid)
	if err != nil {
		return err
	}
	defer session.Close()
	enableCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := h.restoreInternetBeforeProfileEnable(enableCtx, device); err != nil {
		return err
	}
	return session.Enable(enableCtx)
}

func (h *Handler) RenameProfile(ctx context.Context, modemID string, seID string, rawICCID string, nickname string) error {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	iccid, err := sgp22.NewICCID(strings.TrimSpace(rawICCID))
	if err != nil {
		return fmt.Errorf("parse ICCID: %w", err)
	}
	return h.profile.UpdateNickname(device, seID, iccid, nickname)
}

func (h *Handler) RemoveProfile(ctx context.Context, modemID string, seID string, rawICCID string) error {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	iccid, err := sgp22.NewICCID(strings.TrimSpace(rawICCID))
	if err != nil {
		return fmt.Errorf("parse ICCID: %w", err)
	}
	return h.DeleteProfile(ctx, device, seID, iccid)
}
