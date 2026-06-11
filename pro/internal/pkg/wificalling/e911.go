//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/pro/internal/pkg/websheet"
	"github.com/damonto/uicc-go/usim"
	vowifi "github.com/damonto/vowifi-go"
	"github.com/damonto/vowifi-go/wfcsetup"
)

func (c *coordinator) StartEmergencyAddressUpdate(ctx context.Context, modem *mmodem.Modem) (websheet.Info, error) {
	if c.websheets == nil {
		return websheet.Info{}, ErrUnavailable
	}
	card, err := wfcSetupCardFromModem(ctx, modem)
	if err != nil {
		return websheet.Info{}, err
	}
	supported, err := emergencyAddressUpdateAvailable(ctx, card, modem.Logger())
	if err != nil {
		return websheet.Info{}, err
	}
	if !supported {
		return websheet.Info{}, ErrWebsheetUnavailable
	}
	result, err := c.checkEmergencyAddressUpdate(ctx, modem)
	if err != nil {
		return websheet.Info{}, fmt.Errorf("check Wi-Fi Calling E911 setup: %w", err)
	}
	return c.createWFCWebsheet(ctx, result)
}

func (c *coordinator) EmergencyAddressUpdateAvailable(ctx context.Context, modem *mmodem.Modem) bool {
	if c.websheets == nil {
		return false
	}
	card, err := wfcSetupCardFromModem(ctx, modem)
	if err != nil {
		return false
	}
	supported, err := emergencyAddressUpdateAvailable(ctx, card, modem.Logger())
	if err != nil {
		modem.Logger().Debug("check Wi-Fi Calling E911 update support", "error", err)
		return false
	}
	return supported
}

func emergencyAddressUpdateAvailable(ctx context.Context, card wfcsetup.Card, logger *slog.Logger) (bool, error) {
	support, err := wfcsetup.CheckE911UpdateSupport(ctx, wfcsetup.Request{
		Card:   card,
		Logger: logger,
	})
	if err != nil {
		return false, err
	}
	return emergencyAddressUpdateSupported(support), nil
}

func emergencyAddressUpdateSupported(support wfcsetup.E911UpdateSupport) bool {
	return support.Supported && support.BuiltIn && !support.RequiresExternalCredential
}

type modemWFCSetupCard struct {
	sim *mmodem.SIM
}

func wfcSetupCardFromModem(ctx context.Context, modem *mmodem.Modem) (modemWFCSetupCard, error) {
	if modem == nil {
		return modemWFCSetupCard{}, errors.New("modem is required")
	}
	sim := modem.Sim
	if sim == nil {
		var err error
		sim, err = modem.SIMs().Primary(ctx)
		if err != nil {
			return modemWFCSetupCard{}, fmt.Errorf("read primary SIM: %w", err)
		}
	}
	return modemWFCSetupCard{sim: sim}, nil
}

func (c modemWFCSetupCard) ICCID() string {
	if c.sim == nil {
		return ""
	}
	return strings.TrimSpace(c.sim.Identifier)
}

func (c modemWFCSetupCard) IMSI() string {
	if c.sim == nil {
		return ""
	}
	return strings.TrimSpace(c.sim.Imsi)
}

func (c modemWFCSetupCard) MCC() string {
	plmn := strings.TrimSpace(c.simPLMN())
	if len(plmn) >= 3 {
		return plmn[:3]
	}
	imsi := c.IMSI()
	if len(imsi) >= 3 {
		return imsi[:3]
	}
	return ""
}

func (c modemWFCSetupCard) MNC() string {
	plmn := strings.TrimSpace(c.simPLMN())
	if len(plmn) > 3 {
		return plmn[3:]
	}
	imsi := c.IMSI()
	if len(imsi) >= 5 {
		return imsi[3:5]
	}
	return ""
}

func (c modemWFCSetupCard) MNCLength() int {
	plmn := strings.TrimSpace(c.simPLMN())
	if len(plmn) == 5 || len(plmn) == 6 {
		return len(plmn) - 3
	}
	return len(c.MNC())
}

func (c modemWFCSetupCard) GID1() string {
	if c.sim == nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(c.sim.GID1))
}

func (c modemWFCSetupCard) AKA(context.Context, []byte, []byte) (usim.AKAResult, error) {
	return usim.AKAResult{}, errors.New("modem SIM AKA is unavailable")
}

func (c modemWFCSetupCard) simPLMN() string {
	if c.sim == nil {
		return ""
	}
	return strings.TrimSpace(c.sim.OperatorIdentifier)
}

func (c *coordinator) checkEmergencyAddressUpdate(ctx context.Context, modem *mmodem.Modem) (wfcsetup.Result, error) {
	reader, err := openReader(ctx, modem)
	if err != nil {
		return wfcsetup.Result{}, fmt.Errorf("open Wi-Fi Calling SIM reader: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()
	cfg, err := modemClientConfig(ctx, modem)
	if err != nil {
		return wfcsetup.Result{}, err
	}
	card, err := usim.New(ctx, reader, cfg.Logger)
	if err != nil {
		return wfcsetup.Result{}, fmt.Errorf("load Wi-Fi Calling SIM: %w", err)
	}

	return wfcsetup.Check(ctx, wfcsetup.Request{
		Card:    card,
		Device:  wfcSetupDevice(cfg.Terminal),
		Purpose: wfcsetup.PurposeEmergencyAddressUpdate,
		Logger:  cfg.Logger,
	})
}

func wfcSetupDevice(terminal vowifi.TerminalInfo) wfcsetup.Device {
	return wfcsetup.Device{
		IMEI:            strings.TrimSpace(terminal.ID),
		Vendor:          strings.TrimSpace(terminal.Vendor),
		Model:           strings.TrimSpace(terminal.Model),
		SoftwareVersion: strings.TrimSpace(terminal.SoftwareVersion),
	}
}
