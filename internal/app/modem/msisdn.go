package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"

	"github.com/damonto/sigmo/internal/pkg/carrier"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	msisdnclient "github.com/damonto/sigmo/internal/pkg/modem/msisdn"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/sigmo/internal/pkg/phonenumber"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

var errMSISDNInvalidNumber = errors.New("invalid phone number")

var msisdnPhoneRE = regexp.MustCompile(`^\+?[0-9]{1,15}$`)

type msisdn struct {
	newClient  msisdnClientFactory
	openDevice func(*mmodem.Modem) (msisdnDevice, error)
	store      *storage.Store
}

type msisdnDevice interface {
	UpdateMSISDN(ctx context.Context, number string) error
}

type msisdnClient interface {
	Update(string, string) error
	Close() error
}

type msisdnClientFactory func(context.Context, string) (msisdnClient, error)

func newMSISDN(store *storage.Store) *msisdn {
	return &msisdn{
		newClient: func(ctx context.Context, device string) (msisdnClient, error) {
			return msisdnclient.New(ctx, device)
		},
		openDevice: func(modem *mmodem.Modem) (msisdnDevice, error) {
			return mmodem.OpenDevice(modem)
		},
		store: store,
	}
}

func (m *msisdn) Update(ctx context.Context, modem *mmodem.Modem, number string) error {
	number = strings.TrimSpace(number)
	if !msisdnPhoneRE.MatchString(number) {
		return errMSISDNInvalidNumber
	}
	identity := modem.SIMIdentity()
	if identity.ICCID == "" {
		return mmodem.ErrProfileIDMissing
	}
	release, err := modem.ReserveSIMSlot(ctx)
	if err != nil {
		return fmt.Errorf("reserve SIM slot: %w", err)
	}
	defer release()
	if m.store == nil {
		return errors.New("SIM number storage is unavailable")
	}
	if err := modem.UpdateStoredNumber(identity, func(snapshot mmodem.ModemSnapshot) (string, error) {
		region := carrier.Lookup(snapshot.SIM.OperatorIdentifier).Region
		if strings.HasPrefix(snapshot.StoredNumber, "+") && "+"+number == snapshot.StoredNumber {
			number = snapshot.StoredNumber
		}
		number = phonenumber.NormalizeOwnNumber(number, region)
		return number, m.store.SaveSIMProfileNumber(ctx, identity.ICCID, modem.EquipmentIdentifier, number)
	}); err != nil {
		return err
	}

	writeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if modem.MatchesSIMActivation(identity) {
		if err := m.writeCard(writeCtx, modem, identity, number); err != nil {
			slog.Warn("write SIM number to card", "imei", modem.EquipmentIdentifier, "error", err)
		}
	}
	return nil
}

func (m *msisdn) writeCard(ctx context.Context, modem *mmodem.Modem, identity mmodem.SIMIdentity, number string) error {
	device, err := m.openDevice(modem)
	if err != nil {
		return fmt.Errorf("open modem device: %w", err)
	}
	err = device.UpdateMSISDN(ctx, number)
	if errors.Is(err, wwan.ErrUnsupported) {
		if !modem.MatchesSIMActivation(identity) {
			return mmodem.ErrSIMActivationChanged
		}
		return m.updateWithAT(ctx, modem, number)
	}
	return err
}

func (m *msisdn) updateWithAT(ctx context.Context, modem *mmodem.Modem, number string) error {
	port, err := modem.Port(wwanmodem.PortAT)
	if err != nil {
		return fmt.Errorf("find AT port: %w", err)
	}
	client, err := m.newClient(ctx, port.Device)
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
