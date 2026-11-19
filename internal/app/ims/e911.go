package ims

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	imsgo "github.com/damonto/ims-go"
	"github.com/damonto/ims-go/wfcsetup"
	"github.com/damonto/sigmo/internal/app/websheet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	ts43 "github.com/damonto/ts43-go"
	usim "github.com/damonto/wwan-go/sim"
)

func (c *coordinator) StartEmergencyAddressUpdate(ctx context.Context, modem *mmodem.Modem, profileID string, onCallback func(context.Context, websheet.Callback) error) (websheet.Info, error) {
	if c.websheets == nil {
		return websheet.Info{}, ErrUnavailable
	}
	card, err := wfcSetupCardFromModem(ctx, modem)
	if err != nil {
		return websheet.Info{}, err
	}
	supported, err := c.emergencyAddressUpdateAvailable(ctx, card, modem.Logger())
	if err != nil {
		return websheet.Info{}, err
	}
	if !supported {
		return websheet.Info{}, ErrWebsheetUnavailable
	}
	result, underlay, err := c.checkEmergencyAddress(ctx, modem, wfcsetup.PurposeEmergencyAddressUpdate)
	if err != nil {
		return websheet.Info{}, fmt.Errorf("check Wi-Fi Calling E911 setup: %w", err)
	}
	if result.Action == wfcsetup.ActionDisableWFC {
		if err := c.disableWFCForProfile(ctx, modem, profileID); err != nil {
			return websheet.Info{}, err
		}
	}
	if result.Scheme != wfcsetup.SchemeTS43 && result.Scheme != wfcsetup.SchemeNSDS {
		onCallback = nil
	}
	return c.createWFCWebsheet(ctx, result, underlay, onCallback)
}

// The caller serializes callback processing with other operations on this modem.
func emergencyAddressCallback(recheck func(context.Context) error) func(context.Context, websheet.Callback) error {
	var finished bool
	return func(ctx context.Context, callback websheet.Callback) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if finished {
			return nil
		}
		switch wfcWebsheetCallbackResult(callback) {
		case wfcWebsheetCallbackRetry:
			if err := recheck(ctx); err != nil {
				return err
			}
		case wfcWebsheetCallbackDismiss:
		default:
			return nil
		}
		finished = true
		return nil
	}
}

func (c *coordinator) recheckEmergencyAddress(ctx context.Context, modem *mmodem.Modem, profileID string) error {
	if err := checkWebsheetProfile(ctx, modem, profileID); err != nil {
		return err
	}
	result, _, err := c.checkEmergencyAddress(ctx, modem, wfcsetup.PurposeEmergencyAddressRecheck)
	if err != nil {
		return fmt.Errorf("recheck Wi-Fi Calling E911 setup: %w", err)
	}
	switch result.Action {
	case wfcsetup.ActionDisableWFC:
		return c.disableWFCForProfile(ctx, modem, profileID)
	case wfcsetup.ActionWait:
		return ErrWiFiCallingSetupPending
	case wfcsetup.ActionNone:
		if result.Entitled {
			return nil
		}
	}
	return ErrWiFiCallingSetupDenied
}

func checkWebsheetProfile(ctx context.Context, modem *mmodem.Modem, profileID string) error {
	current, err := modem.ProfileID(ctx)
	if err != nil {
		return err
	}
	if current != profileID {
		return websheet.ErrExpired
	}
	return nil
}

func (c *coordinator) disableWFCForProfile(ctx context.Context, modem *mmodem.Modem, profileID string) error {
	if err := checkWebsheetProfile(ctx, modem, profileID); err != nil {
		return err
	}
	settings, err := c.wifiCallingSettings.Get(ctx, profileID)
	if err != nil {
		return err
	}
	settings.Enabled = false
	if err := c.wifiCallingSettings.Put(ctx, profileID, settings); err != nil {
		return err
	}
	modemID := modem.EquipmentIdentifier
	c.mu.Lock()
	session := c.sessions[modemID]
	c.mu.Unlock()
	if session != nil && session.profileID == profileID {
		c.stopAsyncSession(ctx, modemID, session.id)
	}
	return nil
}

func (c *coordinator) EmergencyAddressUpdateAvailable(ctx context.Context, modem *mmodem.Modem) bool {
	if c.websheets == nil {
		return false
	}
	card, err := wfcSetupCardFromModem(ctx, modem)
	if err != nil {
		return false
	}
	supported, err := c.emergencyAddressUpdateAvailable(ctx, card, modem.Logger())
	if err != nil {
		modem.Logger().Debug("check Wi-Fi Calling E911 update support", "error", err)
		return false
	}
	return supported
}

func (c *coordinator) emergencyAddressUpdateAvailable(ctx context.Context, card wfcsetup.Card, logger *slog.Logger) (bool, error) {
	var carriers *ts43.WFCConfigs
	if c.carrierConfig != nil {
		config, err := c.carrierConfig(ctx)
		if err != nil {
			return false, err
		}
		carriers = config.WFC
	}
	return emergencyAddressUpdateAvailable(ctx, card, logger, carriers)
}

func emergencyAddressUpdateAvailable(ctx context.Context, card wfcsetup.Card, logger *slog.Logger, carriers *ts43.WFCConfigs) (bool, error) {
	support, err := wfcsetup.CheckE911UpdateSupport(ctx, wfcsetup.Request{
		Card:   card,
		WFC:    carriers,
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
	sim := modem.Snapshot().SIM
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
	return strings.TrimSpace(c.sim.IMSI)
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

func (c modemWFCSetupCard) simPLMN() string {
	if c.sim == nil {
		return ""
	}
	return strings.TrimSpace(c.sim.OperatorIdentifier)
}

func (c *coordinator) checkEmergencyAddress(ctx context.Context, modem *mmodem.Modem, purpose wfcsetup.Purpose) (result wfcsetup.Result, underlay imsgo.Underlay, err error) {
	reader, err := OpenWWAN(ctx, modem, WWANConfig{Access: AccessWiFiCalling})
	if err != nil {
		return wfcsetup.Result{}, nil, fmt.Errorf("open Wi-Fi Calling SIM reader: %w", err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close Wi-Fi Calling SIM reader: %w", closeErr))
		}
	}()
	cfg, err := c.modemClientConfig(ctx, modem, 0, "")
	if err != nil {
		return wfcsetup.Result{}, nil, err
	}
	card, err := usim.New(ctx, reader, cfg.Logger)
	if err != nil {
		return wfcsetup.Result{}, nil, fmt.Errorf("load Wi-Fi Calling SIM: %w", err)
	}

	result, err = wfcsetup.Check(ctx, wfcsetup.Request{
		Card:            card,
		WFC:             cfg.WFC,
		AuthenticateAKA: card.EAPAKA,
		Device:          wfcSetupDevice(cfg.Terminal),
		Purpose:         purpose,
		Logger:          cfg.Logger,
		HTTPClient:      wifiCallingHTTPClient(cfg.Access.VoWiFi.Underlay),
	})
	return result, cfg.Access.VoWiFi.Underlay, err
}

func wfcSetupDevice(terminal imsgo.TerminalInfo) wfcsetup.Device {
	return wfcsetup.Device{
		IMEI:            strings.TrimSpace(terminal.ID),
		Vendor:          strings.TrimSpace(terminal.Vendor),
		Model:           strings.TrimSpace(terminal.Model),
		SoftwareVersion: strings.TrimSpace(terminal.SoftwareVersion),
	}
}
