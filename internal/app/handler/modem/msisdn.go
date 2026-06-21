package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	msisdnclient "github.com/damonto/sigmo/internal/pkg/modem/msisdn"
)

var errMSISDNInvalidNumber = errors.New("invalid phone number")

var msisdnPhoneRE = regexp.MustCompile(`^\+?[0-9]{1,15}$`)

type msisdn struct {
	newClient  msisdnClientFactory
	refreshSIM func(context.Context, *mmodem.Modem, mmodem.SIMTarget) (*mmodem.Modem, error)
}

type msisdnClient interface {
	Update(string, string) error
	Close() error
}

type msisdnClientFactory func(string) (msisdnClient, error)

func newMSISDN(registry *mmodem.Registry) *msisdn {
	return &msisdn{
		newClient: func(device string) (msisdnClient, error) {
			return msisdnclient.New(device)
		},
		refreshSIM: registry.PowerCycleSIM,
	}
}

func (m *msisdn) Update(ctx context.Context, modem *mmodem.Modem, number string) error {
	number = strings.TrimSpace(number)
	if !msisdnPhoneRE.MatchString(number) {
		return errMSISDNInvalidNumber
	}
	port, err := modem.Port(mmodem.ModemPortTypeAt)
	if err != nil {
		return fmt.Errorf("find AT port: %w", err)
	}
	client, err := m.newClient(port.Device)
	if err != nil {
		return fmt.Errorf("open MSISDN client: %w", err)
	}
	clientClosed := false
	closeClient := func() {
		if clientClosed {
			return
		}
		clientClosed = true
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close MSISDN client", "error", cerr, "imei", modem.EquipmentIdentifier)
		}
	}
	defer func() {
		closeClient()
	}()

	if err := client.Update("", number); err != nil {
		return fmt.Errorf("update MSISDN: %w", err)
	}
	closeClient()

	target := mmodem.SIMTarget{Slot: modem.PrimarySimSlot}
	if modem.Sim != nil {
		target.ICCID = modem.Sim.Identifier
	}
	_, err = m.refreshSIM(ctx, modem, target)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("wait for modem: %w", err)
		}
		return fmt.Errorf("refresh SIM after MSISDN update: %w", err)
	}
	return nil
}
