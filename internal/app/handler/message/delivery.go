package message

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

var (
	errRecipientRequired = errors.New("recipient is required")
	errTextRequired      = errors.New("text is required")
)

func (m *message) Send(ctx context.Context, modem *mmodem.Modem, to string, text string) (string, error) {
	if strings.TrimSpace(to) == "" {
		return "", errRecipientRequired
	}
	if strings.TrimSpace(text) == "" {
		return "", errTextRequired
	}
	to, err := normalizeRecipient(ctx, modem, to)
	if err != nil {
		return "", err
	}
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return "", err
	}
	settings, err := m.wifiCalling.Status(ctx, modem)
	if err != nil && !errors.Is(err, wificalling.ErrUnavailable) {
		return "", fmt.Errorf("read wifi calling status: %w", err)
	}
	if settings.Preferred && settings.Connected {
		msg, err := m.wifiCalling.SendSMS(ctx, modem, to, text)
		if err != nil {
			return "", fmt.Errorf("send SMS to %s over wifi calling: %w", to, err)
		}
		if _, err := m.store.InsertMessage(ctx, msg); err != nil {
			return "", err
		}
		return to, nil
	}
	sms, err := modem.Messaging().Send(ctx, to, text)
	if err != nil {
		if settings.Connected {
			msg, werr := m.wifiCalling.SendSMS(ctx, modem, to, text)
			if werr == nil {
				_, ierr := m.store.InsertMessage(ctx, msg)
				return to, ierr
			}
		}
		return "", fmt.Errorf("send SMS to %s: %w", to, err)
	}
	if _, err := m.store.InsertMessage(ctx, messageFromModemSMS(modem, profileID, sms)); err != nil {
		return "", err
	}
	return to, nil
}
