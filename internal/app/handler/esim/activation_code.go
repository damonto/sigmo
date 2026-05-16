package esim

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	elpa "github.com/damonto/euicc-go/lpa"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func buildActivationCode(ctx context.Context, modem *mmodem.Modem, start downloadClientMessage) (*elpa.ActivationCode, error) {
	smdpURL, err := parseSMDP(start.SMDP)
	if err != nil {
		return nil, err
	}
	matchingID := strings.TrimSpace(start.ActivationCode)
	imei, err := modem.ThreeGPP().IMEI(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading modem IMEI: %w", err)
	}
	return &elpa.ActivationCode{
		SMDP:             smdpURL,
		MatchingID:       matchingID,
		IMEI:             imei,
		ConfirmationCode: strings.TrimSpace(start.ConfirmationCode),
	}, nil
}

func parseSMDP(raw string) (*url.URL, error) {
	smdp := strings.TrimSpace(raw)
	if smdp == "" {
		return nil, errors.New("smdp is required")
	}
	if !strings.Contains(smdp, "://") {
		smdp = "https://" + smdp
	}
	parsed, err := url.Parse(smdp)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid smdp %q", raw)
	}
	return &url.URL{Scheme: "https", Host: parsed.Host}, nil
}
