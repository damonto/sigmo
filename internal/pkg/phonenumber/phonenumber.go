package phonenumber

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nyaruka/phonenumbers"

	"github.com/damonto/sigmo/internal/pkg/carrier"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const unknownRegion = "UN"

var (
	ErrRequired      = errors.New("phone number is required")
	ErrInvalid       = errors.New("invalid phone number")
	ErrModemRequired = errors.New("modem is required")

	shortCodeRE = regexp.MustCompile(`^[0-9]{1,6}$`)
	e164RE      = regexp.MustCompile(`^\+[1-9][0-9]{1,14}$`)
)

func Normalize(ctx context.Context, modem *mmodem.Modem, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrRequired
	}
	if shortCodeRE.MatchString(value) {
		return value, nil
	}
	if strings.HasPrefix(value, "+") {
		return NormalizeForRegion(value, unknownRegion)
	}
	region, err := Region(ctx, modem)
	if err != nil {
		return "", err
	}
	return NormalizeForRegion(value, region)
}

func NormalizeForRegion(value string, region string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrRequired
	}
	if shortCodeRE.MatchString(value) {
		return value, nil
	}

	region = strings.ToUpper(strings.TrimSpace(region))
	if region == "" {
		region = unknownRegion
	}
	if !strings.HasPrefix(value, "+") && region == unknownRegion {
		return "", ErrInvalid
	}
	parseRegion := region
	if parseRegion == unknownRegion {
		parseRegion = "ZZ"
	}

	number, err := phonenumbers.Parse(value, parseRegion)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if !phonenumbers.IsValidNumber(number) {
		return "", ErrInvalid
	}
	e164 := phonenumbers.Format(number, phonenumbers.E164)
	if !e164RE.MatchString(e164) {
		return "", ErrInvalid
	}
	return e164, nil
}

func Display(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || shortCodeRE.MatchString(value) {
		return value
	}
	if formatted := formatNANPString(value); formatted != "" {
		return formatted
	}
	number, err := phonenumbers.Parse(value, "ZZ")
	if err != nil || !phonenumbers.IsValidNumber(number) {
		return value
	}
	return phonenumbers.Format(number, phonenumbers.INTERNATIONAL)
}

func formatNANPString(value string) string {
	national, ok := strings.CutPrefix(value, "+1")
	if !ok {
		return ""
	}
	if len(national) != 10 {
		return ""
	}
	if _, err := strconv.Atoi(national); err != nil {
		return ""
	}
	return fmt.Sprintf("+1 (%s) %s-%s", national[:3], national[3:6], national[6:])
}

func Region(ctx context.Context, modem *mmodem.Modem) (string, error) {
	if modem == nil {
		return "", ErrModemRequired
	}
	sim, err := modem.SIMs().Primary(ctx)
	if err != nil {
		return "", fmt.Errorf("read primary SIM: %w", err)
	}
	return carrier.Lookup(sim.OperatorIdentifier).Region, nil
}
