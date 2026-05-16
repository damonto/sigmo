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
	store        *config.Store
	newClient    msisdnClientFactory
	restartModem func(context.Context, *mmodem.Modem, bool) error
	waitForModem func(context.Context, *mmodem.Modem, func() error) (*mmodem.Modem, error)
}

type msisdnClient interface {
	Update(string, string) error
	Close() error
}

type msisdnClientFactory func(string) (msisdnClient, error)

func newMSISDN(store *config.Store, registry *mmodem.Registry) *msisdn {
	return &msisdn{
		store: store,
		newClient: func(device string) (msisdnClient, error) {
			return msisdnclient.New(device)
		},
		restartModem: func(ctx context.Context, modem *mmodem.Modem, compatible bool) error {
			return modem.Restart(ctx, compatible)
		},
		waitForModem: registry.WaitForModemAfter,
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
			slog.Warn("failed to close MSISDN client", "error", cerr, "modem", modem.EquipmentIdentifier)
		}
	}
	defer func() {
		closeClient()
	}()

	_, err = m.waitForModem(ctx, modem, func() error {
		if err := client.Update("", number); err != nil {
			return fmt.Errorf("update MSISDN: %w", err)
		}
		closeClient()
		if err := m.restartModem(ctx, modem, m.store.FindModem(modem.EquipmentIdentifier).Compatible); err != nil {
			err = fmt.Errorf("restart modem: %w", err)
			if mmodem.IsTransientRestartError(err) {
				return mmodem.ReloadStarted(err)
			}
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("wait for modem: %w", err)
		}
		return err
	}
	return nil
}
