package esim

import (
	"errors"
	"fmt"
	"unicode/utf8"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type profile struct {
	store *settings.Store
}

var errInvalidNickname = errors.New("nickname must be valid utf-8 and 64 bytes or fewer")

func newProfile(store *settings.Store) *profile {
	return &profile{store: store}
}

func (p *profile) List(modem *mmodem.Modem) ([]ProfileResponse, error) {
	current := p.store.Snapshot()
	client, err := lpa.New(modem, &current)
	if err != nil {
		return nil, fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			client.Logger().Warn("close LPA client", "error", cerr)
		}
	}()

	profiles, err := client.ListProfile(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}

	response := make([]ProfileResponse, 0, len(profiles))
	for _, item := range profiles {
		response = append(response, profileResponseFrom(item))
	}
	return response, nil
}

func (p *profile) UpdateNickname(modem *mmodem.Modem, iccid sgp22.ICCID, nickname string) error {
	if err := validateNickname(nickname); err != nil {
		return err
	}
	current := p.store.Snapshot()
	client, err := lpa.New(modem, &current)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			client.Logger().Warn("close LPA client", "error", cerr)
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
