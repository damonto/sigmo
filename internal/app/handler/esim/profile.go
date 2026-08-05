package esim

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"unicode/utf8"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type profile struct {
	store     *settings.Store
	reminders *reminder.Scheduler
	clients   *lpa.Pool
}

var errInvalidNickname = errors.New("nickname must be valid utf-8 and 64 bytes or fewer")

func newProfile(store *settings.Store, reminders *reminder.Scheduler, clients *lpa.Pool) *profile {
	return &profile{store: store, reminders: reminders, clients: clients}
}

func (p *profile) List(ctx context.Context, modem *mmodem.Modem) (*ProfilesResponse, error) {
	ses, err := p.clients.SecureElements(ctx, modem)
	if err != nil {
		return nil, fmt.Errorf("discover eUICC SEs: %w", err)
	}
	response := &ProfilesResponse{SEs: make([]ProfileGroupResponse, 0, len(ses))}
	for _, se := range ses {
		group := ProfileGroupResponse{
			ID:       se.ID,
			Label:    se.Label,
			AID:      hex.EncodeToString(se.AID),
			Profiles: []ProfileResponse{},
		}
		client, err := p.clients.Acquire(ctx, modem, se.ID)
		if err != nil {
			modem.Logger().Warn("create LPA client for eSIM profiles", "seId", se.ID, "error", err)
			return nil, fmt.Errorf("create LPA client for %s: %w", se.ID, err)
		}
		eid, err := client.EID()
		if err != nil {
			if cerr := client.Close(); cerr != nil {
				client.Logger().Warn("close LPA client", "error", cerr)
			}
			err = fmt.Errorf("read EID for %s: %w", se.ID, err)
			modem.Logger().Warn("read eUICC EID for eSIM profiles", "seId", se.ID, "error", err)
			return nil, err
		}
		group.EID = hex.EncodeToString(eid)
		profiles, err := client.ListProfile(nil, nil)
		if err != nil {
			if cerr := client.Close(); cerr != nil {
				client.Logger().Warn("close LPA client", "error", cerr)
			}
			err = fmt.Errorf("list profiles for %s: %w", se.ID, err)
			modem.Logger().Warn("list eSIM profiles", "seId", se.ID, "error", err)
			return nil, err
		}
		for _, item := range profiles {
			profileResponse := profileResponseFrom(item, se.ID, se.Label, group.EID)
			if p.reminders != nil {
				value, ok, err := p.reminders.Get(ctx, reminder.ProfileTypeESIM, profileResponse.ICCID)
				if err != nil {
					if cerr := client.Close(); cerr != nil {
						client.Logger().Warn("close LPA client", "error", cerr)
					}
					return nil, fmt.Errorf("read reminder for %s: %w", profileResponse.ICCID, err)
				}
				if ok {
					details := reminder.DetailsFrom(value)
					profileResponse.Reminder = &details
				}
			}
			group.Profiles = append(group.Profiles, profileResponse)
		}
		if cerr := client.Close(); cerr != nil {
			client.Logger().Warn("close LPA client", "error", cerr)
		}
		response.SEs = append(response.SEs, group)
	}
	return response, nil
}

func (p *profile) UpdateNickname(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID, nickname string) error {
	if err := validateNickname(nickname); err != nil {
		return err
	}
	client, err := p.clients.Acquire(ctx, modem, seID)
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
