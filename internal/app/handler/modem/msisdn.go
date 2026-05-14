package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/config"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	msisdnclient "github.com/damonto/sigmo/internal/pkg/modem/msisdn"
)

var errMSISDNInvalidNumber = errors.New("invalid phone number")

var msisdnPhoneRE = regexp.MustCompile(`^\+?[0-9]{1,15}$`)

type msisdn struct {
	store   *config.Store
	manager *mmodem.Manager
}

func newMSISDN(store *config.Store, manager *mmodem.Manager) *msisdn {
	return &msisdn{
		store:   store,
		manager: manager,
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
	client, err := msisdnclient.New(port.Device)
	if err != nil {
		return fmt.Errorf("open MSISDN client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close MSISDN client", "error", cerr, "modem", modem.EquipmentIdentifier)
		}
	}()
	if err := client.Update("", number); err != nil {
		return fmt.Errorf("update MSISDN: %w", err)
	}
	if err := modem.Restart(m.store.FindModem(modem.EquipmentIdentifier).Compatible); err != nil {
		return fmt.Errorf("restart modem: %w", err)
	}
	_, err = m.manager.WaitForModem(ctx, modem)
	if err != nil {
		return fmt.Errorf("wait for modem: %w", err)
	}
	return nil
}
