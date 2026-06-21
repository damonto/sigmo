package esim

import (
	"context"
	"fmt"
	"strings"

	elpa "github.com/damonto/euicc-go/lpa"

	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func buildActivationCode(ctx context.Context, modem *mmodem.Modem, start downloadClientMessage) (*elpa.ActivationCode, error) {
	var smdp lpa.SMDPAddress
	if err := smdp.UnmarshalText([]byte(start.SMDP)); err != nil {
		return nil, err
	}
	matchingID := strings.TrimSpace(start.ActivationCode)
	imei, err := modem.ThreeGPP().IMEI(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading modem IMEI: %w", err)
	}
	return &elpa.ActivationCode{
		SMDP:             smdp.URL(),
		MatchingID:       matchingID,
		IMEI:             imei,
		ConfirmationCode: strings.TrimSpace(start.ConfirmationCode),
	}, nil
}
