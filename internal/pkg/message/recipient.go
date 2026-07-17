package message

import (
	"context"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/phonenumber"
)

func normalizeSMSAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrRecipientRequired
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && b.Len() == 0:
			b.WriteRune(r)
		case r == ' ', r == '-', r == '.', r == '(', r == ')':
		default:
			return "", ErrRecipientInvalid
		}
	}
	recipient := b.String()
	if recipient == "" {
		return "", ErrRecipientRequired
	}
	if recipient == "+" {
		return "", ErrRecipientInvalid
	}
	return recipient, nil
}

// CanonicalAddress returns E.164 when value is a valid phone number for the
// modem's region. SMS also permits short codes and carrier-specific addresses,
// so values that cannot be interpreted as phone numbers must remain usable.
func CanonicalAddress(ctx context.Context, modem *mmodem.Modem, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	region, err := phonenumber.Region(ctx, modem)
	if err != nil {
		region = ""
	}
	return canonicalAddressForRegion(value, region)
}

func canonicalAddressForRegion(value string, region string) string {
	normalized, err := phonenumber.NormalizeForRegion(value, region)
	if err != nil {
		return value
	}
	return normalized
}
