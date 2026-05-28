package message

import (
	"context"
	"errors"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/phonenumber"
)

var errRecipientInvalid = errors.New("invalid recipient")

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
		return "", errRecipientRequired
	case errors.Is(err, phonenumber.ErrInvalid):
		return "", errRecipientInvalid
	default:
		return "", err
	}
}
