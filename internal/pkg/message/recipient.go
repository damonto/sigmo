package message

import (
	"context"
	"errors"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/phonenumber"
)

func normalizeRecipient(ctx context.Context, modem *mmodem.Modem, to string) (string, error) {
	normalized, err := phonenumber.Normalize(ctx, modem, to)
	return mapPhoneNumberError(normalized, err)
}

func normalizeRecipientForRegion(to string, region string) (string, error) {
	normalized, err := phonenumber.NormalizeForRegion(to, region)
	return mapPhoneNumberError(normalized, err)
}

func mapPhoneNumberError(normalized string, err error) (string, error) {
	switch {
	case err == nil:
		return normalized, nil
	case errors.Is(err, phonenumber.ErrRequired):
		return "", ErrRecipientRequired
	case errors.Is(err, phonenumber.ErrInvalid):
		return "", ErrRecipientInvalid
	default:
		return "", err
	}
}
