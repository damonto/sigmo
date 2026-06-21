package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/damonto/uicc-go/qcom/qmi"
	"github.com/damonto/uicc-go/qcom/uim"
)

const qmiMaxSIMSlot = 5

var (
	qmiPowerRestoreTimeout = 5 * time.Second
	qmiSIMPowerCycleDelay  = 100 * time.Millisecond
)

type qmiUIMReader interface {
	PowerOffSIM(ctx context.Context, slot uint8) error
	PowerOnSIM(ctx context.Context, req uim.PowerOnSIMRequest) error
	SlotStatus(ctx context.Context) (uim.SlotStatus, error)
	CardStatus(ctx context.Context) (uim.CardStatus, error)
	ChangeProvisioningSession(ctx context.Context, req uim.ChangeProvisioningSessionRequest) error
	Close() error
}

var openQMIUIMReader = openUICCQMIUIMReader

func qmiActivateProvisioningIfSimMissing(ctx context.Context, m *Modem, slot uint8) error {
	if slot == 0 {
		return errors.New("QMI SIM slot is required")
	}
	reader, err := openQMIUIMReader(ctx, m.PrimaryPort, slot)
	if err != nil {
		return fmt.Errorf("open QMI UIM reader: %w", err)
	}
	defer closeQMIUIMReader(reader)

	status, err := reader.CardStatus(ctx)
	if err != nil {
		return fmt.Errorf("read QMI UIM card status: %w", err)
	}
	card, ok := qmiCardForSlot(status, slot)
	if !ok {
		return fmt.Errorf("QMI UIM card status missing slot %d", slot)
	}
	app, ok := qmiUSIMApplication(card)
	if !ok {
		return fmt.Errorf("QMI UIM USIM application missing in slot %d", slot)
	}
	if qmiUSIMReady(card, app) {
		return nil
	}
	if len(app.AID) == 0 {
		return errors.New("QMI UIM USIM application AID is empty")
	}

	slog.Info(
		"sim missing, activate provisioning session",
		"imei", m.EquipmentIdentifier,
		"slot", slot,
		"applicationState", app.State,
		"personalizationState", app.PersonalizationState,
	)
	err = reader.ChangeProvisioningSession(ctx, uim.ChangeProvisioningSessionRequest{
		Session:  uim.SessionPrimaryGWProvisioning,
		Activate: true,
		Slot:     slot,
		AID:      app.AID,
	})
	if err != nil {
		return fmt.Errorf("activate provisioning session: %w", err)
	}
	return nil
}

func qmiRepowerSimCard(ctx context.Context, m *Modem, slot uint8) error {
	if slot == 0 {
		return errors.New("QMI SIM slot is required")
	}
	reader, err := openQMIUIMReader(ctx, m.PrimaryPort, slot)
	if err != nil {
		return fmt.Errorf("open QMI UIM reader: %w", err)
	}
	defer closeQMIUIMReader(reader)

	if err := reader.PowerOffSIM(ctx, slot); err != nil {
		return fmt.Errorf("power off sim: %w", err)
	}
	slog.Info("sim powered off", "imei", m.EquipmentIdentifier, "slot", slot)
	if err := qmiWaitFixedDelay(context.Background(), qmiSIMPowerCycleDelay); err != nil {
		err = fmt.Errorf("wait after sim power off: %w", err)
		if powerOnErr := qmiPowerOnSIM(context.Background(), reader, slot); powerOnErr != nil {
			return errors.Join(err, fmt.Errorf("power on sim after power-off wait failure: %w", powerOnErr))
		}
		slog.Warn("sim power-off wait failed, powered sim back on", "imei", m.EquipmentIdentifier, "slot", slot, "error", err)
		return err
	}
	if err := qmiPowerOnSIM(context.Background(), reader, slot); err != nil {
		return fmt.Errorf("power on sim: %w", err)
	}
	slog.Info("sim powered on", "imei", m.EquipmentIdentifier, "slot", slot)
	return nil
}

func qmiPowerOnSIM(ctx context.Context, reader qmiUIMReader, slot uint8) error {
	powerCtx, cancel := context.WithTimeout(ctx, qmiPowerRestoreTimeout)
	defer cancel()
	return reader.PowerOnSIM(powerCtx, uim.PowerOnSIMRequest{Slot: slot})
}

func qmiWaitFixedDelay(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func qmiSIMSlot(m *Modem) (uint8, error) {
	// QMI SIM slots are 1-based; ModemManager returns 0 when slots aren't supported.
	if m.PrimarySimSlot == 0 {
		return 1, nil
	}
	if m.PrimarySimSlot > qmiMaxSIMSlot {
		return 0, fmt.Errorf("QMI SIM slot %d is out of range", m.PrimarySimSlot)
	}
	return uint8(m.PrimarySimSlot), nil
}

func openUICCQMIUIMReader(ctx context.Context, device string, slot uint8) (qmiUIMReader, error) {
	transport, err := qmi.Open(ctx, qmi.WithProxy(device))
	if err != nil {
		return nil, err
	}
	reader, err := uim.New(ctx, transport, uim.WithSlot(slot))
	if err != nil {
		_ = transport.Close()
		return nil, err
	}
	return reader, nil
}

func closeQMIUIMReader(reader qmiUIMReader) {
	if err := reader.Close(); err != nil {
		slog.Debug("close QMI UIM reader", "error", err)
	}
}

func qmiCardForSlot(status uim.CardStatus, slot uint8) (uim.Card, bool) {
	index := int(slot) - 1
	if index < 0 || index >= len(status.Cards) {
		return uim.Card{}, false
	}
	return status.Cards[index], true
}

func qmiUSIMApplication(card uim.Card) (uim.CardApplication, bool) {
	for _, app := range card.Applications {
		if app.Type == uim.ApplicationTypeUSIM {
			return app, true
		}
	}
	return uim.CardApplication{}, false
}

func qmiUSIMReady(card uim.Card, app uim.CardApplication) bool {
	return card.State == uim.CardStatePresent &&
		app.Type == uim.ApplicationTypeUSIM &&
		app.State == uim.ApplicationStateReady &&
		app.PersonalizationState == uim.PersonalizationStateReady
}

func qmiUSIMPresentForSlot(status uim.CardStatus, slot uint8) bool {
	card, ok := qmiCardForSlot(status, slot)
	if !ok || card.State != uim.CardStatePresent {
		return false
	}
	_, ok = qmiUSIMApplication(card)
	return ok
}

func qmiUSIMReadyForSlot(status uim.CardStatus, slot uint8) bool {
	card, ok := qmiCardForSlot(status, slot)
	if !ok {
		return false
	}
	app, ok := qmiUSIMApplication(card)
	return ok && qmiUSIMReady(card, app)
}
