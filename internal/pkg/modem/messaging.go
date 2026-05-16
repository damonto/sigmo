package modem

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/godbus/dbus/v5"
)

const ModemMessagingInterface = ModemInterface + ".Messaging"

type Messaging struct {
	modem *Modem
}

func (m *Modem) Messaging() *Messaging {
	return &Messaging{modem: m}
}

func (msg *Messaging) List(ctx context.Context) ([]*SMS, error) {
	var messages []dbus.ObjectPath
	var err error
	err = msg.modem.dbusObject.CallWithContext(ctx, ModemMessagingInterface+".List", 0).Store(&messages)
	s := make([]*SMS, len(messages))
	for i, message := range messages {
		s[i], err = msg.Retrieve(ctx, message)
		if err != nil {
			return nil, err
		}
	}
	return s, err
}

func (msg *Messaging) Create(ctx context.Context, to string, text string) (dbus.ObjectPath, error) {
	var path dbus.ObjectPath
	data := map[string]any{
		"number": to,
		"text":   text,
	}
	err := msg.modem.dbusObject.CallWithContext(ctx, ModemMessagingInterface+".Create", 0, &data).Store(&path)
	return path, err
}

func (msg *Messaging) Delete(ctx context.Context, path dbus.ObjectPath) error {
	return msg.modem.dbusObject.CallWithContext(ctx, ModemMessagingInterface+".Delete", 0, path).Err
}

func (msg *Messaging) SetDefaultStorage(ctx context.Context, storage SMSStorage) error {
	return msg.modem.dbusObject.CallWithContext(ctx, ModemMessagingInterface+".SetDefaultStorage", 0, uint32(storage)).Err
}

func (msg *Messaging) SupportedStorages(ctx context.Context) ([]SMSStorage, error) {
	variant, err := dbusProperty(ctx, msg.modem.dbusObject, ModemMessagingInterface, "SupportedStorages")
	if err != nil {
		return nil, fmt.Errorf("read supported SMS storages: %w", err)
	}
	return smsStoragesFromVariant(variant), nil
}

func (msg *Messaging) DefaultStorage(ctx context.Context) (SMSStorage, error) {
	variant, err := dbusProperty(ctx, msg.modem.dbusObject, ModemMessagingInterface, "DefaultStorage")
	if err != nil {
		return SMSStorageUnknown, fmt.Errorf("read default SMS storage: %w", err)
	}
	return SMSStorage(uintFromVariant[uint32](variant)), nil
}

func smsStoragesFromVariant(variant dbus.Variant) []SMSStorage {
	values, ok := variant.Value().([]uint32)
	if !ok {
		return nil
	}
	storages := make([]SMSStorage, len(values))
	for i, value := range values {
		storages[i] = SMSStorage(value)
	}
	return storages
}

func (msg *Messaging) Subscribe(ctx context.Context, subscriber func(message *SMS) error) error {
	dbusConn, err := systemBusPrivate()
	if err != nil {
		return err
	}
	defer func() {
		if err := dbusConn.Close(); err != nil {
			slog.Error("close dbus connection", "error", err)
		}
	}()
	if err := dbusConn.AddMatchSignal(
		dbus.WithMatchMember("Added"),
		dbus.WithMatchPathNamespace(msg.modem.objectPath),
	); err != nil {
		return err
	}
	signalChan := make(chan *dbus.Signal, 10)
	dbusConn.Signal(signalChan)
	defer dbusConn.RemoveSignal(signalChan)
	for {
		select {
		case sig, ok := <-signalChan:
			if !ok {
				return nil
			}
			path, received, ok := receivedMessageSignal(sig)
			if !ok {
				slog.Warn("ignore invalid messaging signal", "path", msg.modem.objectPath, "body", signalBody(sig))
				continue
			}
			if !received {
				continue
			}
			s, err := msg.waitForSMSReceived(ctx, path, 100*time.Millisecond)
			if err != nil {
				slog.Error("process message", "error", err, "path", sig.Path)
				continue
			}
			if err := subscriber(s); err != nil {
				slog.Error("process message", "error", err, "path", sig.Path)
			}
		case <-ctx.Done():
			slog.Info("unsubscribing from modem messaging", "path", msg.modem.dbusObject.Path())
			return nil
		}
	}
}

func receivedMessageSignal(sig *dbus.Signal) (dbus.ObjectPath, bool, bool) {
	if sig == nil || len(sig.Body) < 2 {
		return "", false, false
	}
	path, ok := sig.Body[0].(dbus.ObjectPath)
	if !ok || path == "" {
		return "", false, false
	}
	received, ok := sig.Body[1].(bool)
	if !ok {
		return "", false, false
	}
	return path, received, true
}

func signalBody(sig *dbus.Signal) []any {
	if sig == nil {
		return nil
	}
	return sig.Body
}

func (msg *Messaging) waitForSMSReceived(ctx context.Context, path dbus.ObjectPath, interval time.Duration) (*SMS, error) {
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s, err := msg.Retrieve(ctx, path)
		if err != nil {
			return nil, err
		}
		if s.State == SMSStateReceived {
			return s, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
