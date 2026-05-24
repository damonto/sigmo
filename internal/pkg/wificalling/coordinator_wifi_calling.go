//go:build wifi_calling

package wificalling

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	imsclient "github.com/damonto/ims-client"
	"github.com/damonto/ims-client/driver/at"
	"github.com/damonto/ims-client/driver/mbim"
	"github.com/damonto/ims-client/driver/qmi"
	usimreader "github.com/damonto/ims-client/usim/reader"
	"github.com/godbus/dbus/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type Config struct {
	Store      *storage.Store
	OnIncoming IncomingSMSFunc
}

type coordinator struct {
	settings   *SettingsStore
	onIncoming IncomingSMSFunc

	mu       sync.Mutex
	sessions map[string]*sessionState
}

type sessionState struct {
	cancel    context.CancelFunc
	client    *imsclient.Client
	ussd      *imsclient.Session
	modemPath dbus.ObjectPath
	profileID string
	connected bool
}

var retryDelays = []time.Duration{
	30 * time.Second,
	60 * time.Second,
	120 * time.Second,
	240 * time.Second,
	300 * time.Second,
}

func New(cfg Config) Coordinator {
	return &coordinator{
		settings:   NewSettingsStore(cfg.Store),
		onIncoming: cfg.OnIncoming,
		sessions:   make(map[string]*sessionState),
	}
}

func (c *coordinator) Run(ctx context.Context, registry *mmodem.Registry) error {
	if err := c.startEnabled(ctx, registry); err != nil {
		slog.Warn("start configured Wi-Fi Calling profiles", "error", err)
	}
	unsubscribe, err := registry.Subscribe(func(event mmodem.ModemEvent) error {
		switch event.Type {
		case mmodem.ModemEventAdded:
			if event.Modem != nil {
				c.startIfEnabled(ctx, event.Modem)
			}
		case mmodem.ModemEventRemoved:
			c.stopByPath(event.Path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("subscribe modem registry: %w", err)
	}
	defer unsubscribe()
	<-ctx.Done()
	c.stopAll()
	return nil
}

func (c *coordinator) Settings(ctx context.Context, modem *mmodem.Modem) (Settings, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return Settings{}, err
	}
	return c.settings.Get(ctx, profileID)
}

func (c *coordinator) UpdateSettings(ctx context.Context, modem *mmodem.Modem, settings Settings) error {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return err
	}
	if err := c.settings.Put(ctx, profileID, settings); err != nil {
		return err
	}
	c.stop(modem.EquipmentIdentifier)
	if settings.Enabled {
		c.start(modem, profileID)
	}
	return nil
}

func (c *coordinator) Status(ctx context.Context, modem *mmodem.Modem) (Status, error) {
	settings, err := c.Settings(ctx, modem)
	if err != nil {
		return Status{}, err
	}
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return Status{}, err
	}
	c.mu.Lock()
	session := c.sessions[modem.EquipmentIdentifier]
	connected := session != nil && session.connected && session.profileID == profileID
	c.mu.Unlock()
	return Status{Settings: settings, Connected: connected}, nil
}

func (c *coordinator) SendSMS(ctx context.Context, modem *mmodem.Modem, to string, text string) (storage.Message, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return storage.Message{}, err
	}
	client, err := c.connectedClient(modem.EquipmentIdentifier, profileID)
	if err != nil {
		return storage.Message{}, err
	}
	submission, err := client.SMS().Send(ctx, to, text)
	if err != nil {
		return storage.Message{}, err
	}
	return storage.Message{
		ProfileID:   profileID,
		Source:      storage.MessageSourceWiFiCalling,
		ExternalKey: submission.ID,
		Sender:      modem.Number,
		Recipient:   strings.TrimSpace(to),
		Text:        text,
		Timestamp:   submission.SubmittedAt,
		Status:      "sent",
		Incoming:    false,
		WiFiCalling: true,
	}, nil
}

func (c *coordinator) ExecuteUSSD(ctx context.Context, modem *mmodem.Modem, action string, code string) (string, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return "", err
	}
	client, err := c.connectedClient(modem.EquipmentIdentifier, profileID)
	if err != nil {
		return "", err
	}
	switch action {
	case actionUSSDInitialize:
		session, err := client.USSD().Start()
		if err != nil {
			return "", err
		}
		result, err := session.Send(ctx, code)
		if err != nil {
			return "", err
		}
		c.setUSSDSession(modem.EquipmentIdentifier, session, result.Closed)
		return result.Message.Text, nil
	case actionUSSDReply:
		session, err := c.ussdSession(modem.EquipmentIdentifier)
		if err != nil {
			return "", err
		}
		result, err := session.Reply(ctx, code)
		if err != nil {
			return "", err
		}
		c.setUSSDSession(modem.EquipmentIdentifier, session, result.Closed)
		return result.Message.Text, nil
	default:
		return "", errors.New("action must be initialize or reply")
	}
}

func (c *coordinator) ussdSession(modemID string) (*imsclient.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.ussd == nil {
		return nil, imsclient.ErrUSSDNotStarted
	}
	return session.ussd, nil
}

func (c *coordinator) setUSSDSession(modemID string, ussd *imsclient.Session, closed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil {
		return
	}
	if closed {
		session.ussd = nil
		return
	}
	session.ussd = ussd
}

func (c *coordinator) startEnabled(ctx context.Context, registry *mmodem.Registry) error {
	modems, err := registry.Modems(ctx)
	if err != nil {
		return fmt.Errorf("list modems: %w", err)
	}
	for _, modem := range modems {
		c.startIfEnabled(ctx, modem)
	}
	return nil
}

func (c *coordinator) startIfEnabled(ctx context.Context, modem *mmodem.Modem) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		slog.Debug("skip Wi-Fi Calling start", "modem", modem.EquipmentIdentifier, "error", err)
		return
	}
	settings, err := c.settings.Get(ctx, profileID)
	if err != nil {
		slog.Warn("read Wi-Fi Calling settings", "modem", modem.EquipmentIdentifier, "error", err)
		return
	}
	if settings.Enabled {
		c.start(modem, profileID)
	}
}

func (c *coordinator) start(modem *mmodem.Modem, profileID string) {
	if modem == nil || strings.TrimSpace(modem.EquipmentIdentifier) == "" {
		return
	}
	modemID := modem.EquipmentIdentifier
	c.mu.Lock()
	if current := c.sessions[modemID]; current != nil {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.sessions[modemID] = &sessionState{cancel: cancel, modemPath: modem.Path(), profileID: profileID}
	c.mu.Unlock()
	go c.connectLoop(ctx, modem, profileID)
}

func (c *coordinator) connectLoop(ctx context.Context, modem *mmodem.Modem, profileID string) {
	for attempt := 0; ; attempt++ {
		client, err := c.connectOnce(ctx, modem)
		if err == nil {
			c.markConnected(modem.EquipmentIdentifier, client)
			c.watchClient(ctx, modem, profileID, client)
			return
		}
		if ctx.Err() != nil {
			return
		}
		if attempt >= len(retryDelays) {
			slog.Warn("Wi-Fi Calling connection attempts exhausted", "modem", modem.EquipmentIdentifier, "error", err)
			return
		}
		delay := retryDelays[attempt]
		slog.Warn("Wi-Fi Calling connect", "modem", modem.EquipmentIdentifier, "retryIn", delay, "error", err)
		if err := sleep(ctx, delay); err != nil {
			return
		}
	}
}

func (c *coordinator) connectOnce(ctx context.Context, modem *mmodem.Modem) (*imsclient.Client, error) {
	reader, err := openReader(ctx, modem)
	if err != nil {
		return nil, err
	}
	client, err := imsclient.New(reader, &imsclient.Config{
		IMEI:   modem.EquipmentIdentifier,
		Logger: slog.Default(),
	})
	if err != nil {
		return nil, err
	}
	if err := client.Connect(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *coordinator) watchClient(ctx context.Context, modem *mmodem.Modem, profileID string, client *imsclient.Client) {
	events := client.Events()
	defer events.Close()
	smsEvents := client.SMS().Events()
	defer smsEvents.Close()
	for {
		select {
		case msg, ok := <-smsEvents.Incoming:
			if !ok {
				c.markDisconnected(modem.EquipmentIdentifier, client)
				return
			}
			c.forwardIncoming(ctx, modem, profileID, msg)
		case state, ok := <-events.State:
			if !ok {
				c.markDisconnected(modem.EquipmentIdentifier, client)
				return
			}
			if state == imsclient.StateFailed || state == imsclient.StateClosed {
				_ = client.Close()
				c.markDisconnected(modem.EquipmentIdentifier, client)
				return
			}
		case <-ctx.Done():
			_ = client.Close()
			c.markDisconnected(modem.EquipmentIdentifier, client)
			return
		}
	}
}

func (c *coordinator) forwardIncoming(ctx context.Context, modem *mmodem.Modem, profileID string, msg imsclient.SMS) {
	if c.onIncoming == nil {
		return
	}
	stored := storage.Message{
		ProfileID:   profileID,
		Source:      storage.MessageSourceWiFiCalling,
		ExternalKey: incomingMessageKey(msg),
		Sender:      strings.TrimSpace(msg.From),
		Recipient:   strings.TrimSpace(msg.To),
		Text:        msg.Text,
		Timestamp:   msg.ReceivedAt,
		Status:      "received",
		Incoming:    true,
		WiFiCalling: true,
	}
	if err := c.onIncoming(ctx, IncomingSMS{ModemID: modem.EquipmentIdentifier, Message: stored}); err != nil {
		slog.Warn("forward Wi-Fi Calling SMS", "modem", modem.EquipmentIdentifier, "error", err)
	}
}

func (c *coordinator) connectedClient(modemID string, profileID string) (*imsclient.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || !session.connected || session.client == nil || session.profileID != profileID {
		return nil, ErrNotConnected
	}
	return session.client, nil
}

func (c *coordinator) markConnected(modemID string, client *imsclient.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if session := c.sessions[modemID]; session != nil {
		session.client = client
		session.connected = true
	}
}

func (c *coordinator) markDisconnected(modemID string, client *imsclient.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.client != client {
		return
	}
	session.client = nil
	session.connected = false
}

func (c *coordinator) stop(modemID string) {
	c.mu.Lock()
	session := c.sessions[modemID]
	delete(c.sessions, modemID)
	c.mu.Unlock()
	if session == nil {
		return
	}
	session.cancel()
	if session.client != nil {
		_ = session.client.Close()
	}
}

func (c *coordinator) stopAll() {
	c.mu.Lock()
	ids := make([]string, 0, len(c.sessions))
	for modemID := range c.sessions {
		ids = append(ids, modemID)
	}
	c.mu.Unlock()
	for _, modemID := range ids {
		c.stop(modemID)
	}
}

func (c *coordinator) stopByPath(path dbus.ObjectPath) {
	if path == "" {
		return
	}
	c.mu.Lock()
	var modemIDs []string
	for modemID, session := range c.sessions {
		if session != nil && session.modemPath == path {
			modemIDs = append(modemIDs, modemID)
		}
	}
	c.mu.Unlock()
	for _, modemID := range modemIDs {
		c.stop(modemID)
	}
}

func openReader(ctx context.Context, modem *mmodem.Modem) (usimreader.Reader, error) {
	slot := 1
	if modem.PrimarySimSlot > 0 {
		slot = int(modem.PrimarySimSlot)
	}
	switch modem.PrimaryPortType() {
	case mmodem.ModemPortTypeQmi:
		return qmi.Open(ctx, modem.PrimaryPort, slot)
	case mmodem.ModemPortTypeMbim:
		return mbim.Open(ctx, modem.PrimaryPort, slot)
	case mmodem.ModemPortTypeAt:
		return at.New(modem.PrimaryPort, 0)
	default:
		if port, err := modem.Port(mmodem.ModemPortTypeQmi); err == nil {
			return qmi.Open(ctx, port.Device, slot)
		}
		if port, err := modem.Port(mmodem.ModemPortTypeMbim); err == nil {
			return mbim.Open(ctx, port.Device, slot)
		}
		if port, err := modem.Port(mmodem.ModemPortTypeAt); err == nil {
			return at.New(port.Device, 0)
		}
		return nil, errors.New("Wi-Fi Calling requires QMI, MBIM, or AT modem port")
	}
}

func incomingMessageKey(msg imsclient.SMS) string {
	if callID := strings.TrimSpace(msg.CallID); callID != "" {
		return callID
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		msg.From,
		msg.To,
		msg.ServiceCenter,
		msg.Text,
		msg.ReceivedAt.UTC().Format(time.RFC3339Nano),
	}, "\x00")))
	return "incoming:" + hex.EncodeToString(sum[:])
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
