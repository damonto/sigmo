package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	mdevice "github.com/damonto/sigmo/internal/pkg/modem/device"
	msisdnclient "github.com/damonto/sigmo/internal/pkg/modem/msisdn"
)

var errMSISDNInvalidNumber = errors.New("invalid phone number")

var msisdnPhoneRE = regexp.MustCompile(`^\+?[0-9]{1,15}$`)

type msisdn struct {
	newClient         msisdnClientFactory
	openDevice        func(*mmodem.Modem) (msisdnDevice, error)
	refreshSIMAndWait func(context.Context, *mmodem.Modem, mmodem.SIMTarget) (*mmodem.Modem, error)
}

type msisdnDevice interface {
	UpdateMSISDN(ctx context.Context, number string) error
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
		openDevice: func(modem *mmodem.Modem) (msisdnDevice, error) {
			return mmodem.OpenDevice(modem)
		},
		refreshSIMAndWait: registry.PowerCycleSIMAndWait,
	}
}

func (m *msisdn) Update(ctx context.Context, modem *mmodem.Modem, number string) error {
	number = strings.TrimSpace(number)
	if !msisdnPhoneRE.MatchString(number) {
		return errMSISDNInvalidNumber
	}
	device, err := m.openDevice(modem)
	if err != nil {
		return fmt.Errorf("open modem device: %w", err)
	}
	if err := device.UpdateMSISDN(ctx, number); errors.Is(err, mdevice.ErrUnsupported) {
		if err := m.updateWithAT(modem, number); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("update MSISDN: %w", err)
	}

	target := mmodem.SIMTarget{Slot: modem.PrimarySimSlot}
	if modem.Sim != nil {
		target.ICCID = modem.Sim.Identifier
	}
	if _, err := m.refreshSIMAndWait(ctx, modem, target); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("wait for modem: %w", err)
		}
		return fmt.Errorf("refresh SIM after MSISDN update: %w", err)
	}
	return nil
}

func (m *msisdn) updateWithAT(modem *mmodem.Modem, number string) error {
	port, err := modem.Port(mmodem.ModemPortTypeAt)
	if err != nil {
		return fmt.Errorf("find AT port: %w", err)
	}
	client, err := m.newClient(port.Device)
	if err != nil {
		return fmt.Errorf("open MSISDN client: %w", err)
	}
	if err := client.Update("", number); err != nil {
		if closeErr := client.Close(); closeErr != nil {
			slog.Warn("close MSISDN client", "error", closeErr, "imei", modem.EquipmentIdentifier)
		}
		return fmt.Errorf("update MSISDN: %w", err)
	}
	if err := client.Close(); err != nil {
		slog.Warn("close MSISDN client", "error", err, "imei", modem.EquipmentIdentifier)
	}
	return nil
}
