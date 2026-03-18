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

type Service struct {
	cfg     *config.Config
	manager *mmodem.Manager
}

var errInvalidNickname = errors.New("nickname must be valid utf-8 and 64 bytes or fewer")

func NewService(cfg *config.Config, manager *mmodem.Manager) *Service {
	return &Service{
		cfg:     cfg,
		manager: manager,
	}
}

func (s *Service) List(modem *mmodem.Modem) ([]ProfileResponse, error) {
	client, err := lpa.New(modem, s.cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()

	profiles, err := client.ListProfile(nil, nil)
	if err != nil {
		slog.Error("failed to list profiles", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
	}

	response := make([]ProfileResponse, 0, len(profiles))
	for _, profile := range profiles {
		name := profile.ProfileNickname
		if name == "" {
			name = profile.ProfileName
		}
		carrierInfo := carrier.Lookup(profile.ProfileOwner.MCC() + profile.ProfileOwner.MNC())
		icon := ""
		if fileType := profile.Icon.FileType(); fileType != "" {
			icon = fmt.Sprintf("data:%s;base64,%s", fileType, base64.StdEncoding.EncodeToString(profile.Icon))
		}
		regionCode := carrierInfo.Region
		response = append(response, ProfileResponse{
			Name:                name,
			ServiceProviderName: profile.ServiceProviderName,
			ICCID:               profile.ICCID.String(),
			Icon:                icon,
			ProfileState:        uint8(profile.ProfileState),
			RegionCode:          regionCode,
		})
	}
	return response, nil
}

func (s *Service) UpdateNickname(modem *mmodem.Modem, iccid sgp22.ICCID, nickname string) error {
	if err := validateNickname(nickname); err != nil {
		return err
	}
	client, err := lpa.New(modem, s.cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()

	if err := client.SetNickname(iccid, nickname); err != nil {
		slog.Error("failed to set nickname", "modem", modem.EquipmentIdentifier, "iccid", iccid.String(), "error", err)
		return err
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
