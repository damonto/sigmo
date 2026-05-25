package message

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/nyaruka/phonenumbers"

	"github.com/damonto/sigmo/internal/pkg/carrier"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const unknownRegion = "UN"

var (
	errRecipientInvalid = errors.New("invalid recipient")
	shortCodeRE         = regexp.MustCompile(`^[0-9]{1,6}$`)
	e164RE              = regexp.MustCompile(`^\+[1-9][0-9]{1,14}$`)
)

func normalizeRecipient(ctx context.Context, modem *mmodem.Modem, to string) (string, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return "", errRecipientRequired
	}
	if shortCodeRE.MatchString(to) {
		return to, nil
	}
	region, err := recipientRegion(ctx, modem)
	if err != nil {
		return "", err
	}
	return normalizeRecipientForRegion(to, region)
}

func normalizeRecipientForRegion(to string, region string) (string, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return "", errRecipientRequired
	}
	if shortCodeRE.MatchString(to) {
		return to, nil
	}

	region = strings.ToUpper(strings.TrimSpace(region))
	if region == "" {
		region = unknownRegion
	}
	if !strings.HasPrefix(to, "+") && region == unknownRegion {
		return "", errRecipientInvalid
	}
	parseRegion := region
	if parseRegion == unknownRegion {
		parseRegion = "ZZ"
	}

	number, err := phonenumbers.Parse(to, parseRegion)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errRecipientInvalid, err)
	}
	if !phonenumbers.IsValidNumber(number) {
		return "", errRecipientInvalid
	}
	e164 := phonenumbers.Format(number, phonenumbers.E164)
	if !e164RE.MatchString(e164) {
		return "", errRecipientInvalid
	}
	return e164, nil
}

func recipientRegion(ctx context.Context, modem *mmodem.Modem) (string, error) {
	if modem == nil {
		return "", errors.New("modem is required")
	}
	sim, err := modem.SIMs().Primary(ctx)
	if err != nil {
		return "", fmt.Errorf("read primary SIM: %w", err)
	}
	return carrier.Lookup(sim.OperatorIdentifier).Region, nil
}
