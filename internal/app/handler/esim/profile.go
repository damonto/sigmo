package esim

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"unicode/utf8"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/carrier"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type profile struct {
	store *config.Store
}

var errInvalidNickname = errors.New("nickname must be valid utf-8 and 64 bytes or fewer")

func newProfile(store *config.Store) *profile {
	return &profile{store: store}
}

func (p *profile) List(modem *mmodem.Modem) ([]ProfileResponse, error) {
	cfg := p.store.Snapshot()
	client, err := lpa.New(modem, &cfg)
	if err != nil {
		return nil, fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("close LPA client", "error", cerr)
		}
	}()

	profiles, err := client.ListProfile(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}

	response := make([]ProfileResponse, 0, len(profiles))
	for _, item := range profiles {
		name := item.ProfileNickname
		if name == "" {
			name = item.ProfileName
		}
		carrierInfo := carrier.Lookup(item.ProfileOwner.MCC() + item.ProfileOwner.MNC())
		icon := ""
		if fileType := item.Icon.FileType(); fileType != "" {
			icon = fmt.Sprintf("data:%s;base64,%s", fileType, base64.StdEncoding.EncodeToString(item.Icon))
		}
		regionCode := carrierInfo.Region
		response = append(response, ProfileResponse{
			Name:                name,
			ServiceProviderName: item.ServiceProviderName,
			ICCID:               item.ICCID.String(),
			Icon:                icon,
			ProfileState:        uint8(item.ProfileState),
			RegionCode:          regionCode,
		})
	}
	return response, nil
}

func (p *profile) UpdateNickname(modem *mmodem.Modem, iccid sgp22.ICCID, nickname string) error {
	if err := validateNickname(nickname); err != nil {
		return err
	}
	cfg := p.store.Snapshot()
	client, err := lpa.New(modem, &cfg)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("close LPA client", "error", cerr)
		}
	}()

	if err := client.SetNickname(iccid, nickname); err != nil {
		return fmt.Errorf("set nickname for %s: %w", iccid.String(), err)
	}
	return nil
}

func validateNickname(nickname string) error {
	if !utf8.ValidString(nickname) {
		return errInvalidNickname
	}
	if len(nickname) > 64 {
		return errInvalidNickname
	}
	return nil
}
