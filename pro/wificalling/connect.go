//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	vowifi "github.com/damonto/vowifi-go"
	"github.com/damonto/vowifi-go/wfcsetup"
	"github.com/godbus/dbus/v5"
)

var retryDelays = []time.Duration{
	30 * time.Second,
	60 * time.Second,
	120 * time.Second,
	240 * time.Second,
	300 * time.Second,
	600 * time.Second,
}

const (
	terminalVendor          = "Google"
	terminalModel           = "Pixel 8 Pro"
	terminalSoftwareVersion = "15/AP3A.240905.015"
)

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
		slog.Debug("skip Wi-Fi Calling start", "imei", modem.EquipmentIdentifier, "error", err)
		return
	}
	settings, err := c.settings.Get(ctx, profileID)
	if err != nil {
		slog.Warn("read Wi-Fi Calling settings", "imei", modem.EquipmentIdentifier, "error", err)
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
	done := make(chan struct{})
	c.nextSessionID++
	sessionID := c.nextSessionID
	c.sessions[modemID] = &sessionState{
		id:        sessionID,
		cancel:    cancel,
		done:      done,
		reconnect: make(chan struct{}, 1),
		phase:     sessionPhaseConnecting,
		modemPath: modem.Path(),
		profileID: profileID,
		calls:     make(map[string]*voiceCallState),
	}
	c.mu.Unlock()
	go func() {
		defer close(done)
		c.connectLoop(ctx, modem, profileID, sessionID)
	}()
}

func (c *coordinator) connectLoop(ctx context.Context, modem *mmodem.Modem, profileID string, sessionID uint64) {
	for {
		c.markConnecting(modem.EquipmentIdentifier, sessionID)
		client, err := c.connectWithRetry(ctx, modem, sessionID)
		if err != nil {
			return
		}
		c.markConnected(modem.EquipmentIdentifier, sessionID, client)
		c.watchClient(ctx, modem, profileID, sessionID, client)
		if ctx.Err() != nil {
			return
		}
		c.markConnecting(modem.EquipmentIdentifier, sessionID)
		delay := retryDelays[0]
		slog.Warn("Wi-Fi Calling disconnected", "imei", modem.EquipmentIdentifier, "retryIn", delay)
		if err := sleep(ctx, delay); err != nil {
			return
		}
	}
}

func (c *coordinator) connectWithRetry(ctx context.Context, modem *mmodem.Modem, sessionID uint64) (*vowifi.Client, error) {
	attempt := 0
	for {
		client, err := c.connectOnce(ctx, modem, sessionID)
		if err == nil {
			return client, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, wfcsetup.ErrUserActionRequired) {
			slog.Warn("Wi-Fi Calling requires carrier websheet", "imei", modem.EquipmentIdentifier, "error", err)
			if err := c.waitForWebsheet(ctx, modem.EquipmentIdentifier, sessionID); err != nil {
				if errors.Is(err, ErrWebsheetDismissed) {
					slog.Info("Wi-Fi Calling carrier websheet dismissed", "imei", modem.EquipmentIdentifier)
					c.stopAsyncSession(modem.EquipmentIdentifier, sessionID)
				}
				return nil, err
			}
			attempt = 0
			continue
		}
		if attempt >= len(retryDelays) {
			slog.Warn("Wi-Fi Calling connection attempts exhausted", "imei", modem.EquipmentIdentifier, "error", err)
			return nil, err
		}
		delay := retryDelays[attempt]
		attempt++
		slog.Warn("Wi-Fi Calling connect", "imei", modem.EquipmentIdentifier, "retryIn", delay, "error", err)
		if err := sleep(ctx, delay); err != nil {
			return nil, err
		}
	}
}

func (c *coordinator) connectOnce(ctx context.Context, modem *mmodem.Modem, sessionID uint64) (*vowifi.Client, error) {
	reader, err := openReader(ctx, modem)
	if err != nil {
		return nil, err
	}
	cfg, err := modemClientConfig(ctx, modem)
	if err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	client, err := vowifi.New(reader, cfg)
	if err != nil {
		return nil, err
	}
	if err := client.Connect(ctx); err != nil {
		if req, ok := c.wfcWebsheetRequest(err); ok {
			session, serr := c.websheets.Create(ctx, req)
			if serr != nil {
				_ = client.Close()
				return nil, errors.Join(err, serr)
			}
			c.attachWebsheet(modem.EquipmentIdentifier, sessionID, session)
		}
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func modemClientConfig(ctx context.Context, modem *mmodem.Modem) (*vowifi.Config, error) {
	imei, err := modem.ThreeGPP().IMEI(ctx)
	if err != nil {
		return nil, fmt.Errorf("read modem IMEI: %w", err)
	}
	return modemClientConfigForIMEI(imei), nil
}

func modemClientConfigForIMEI(imei string) *vowifi.Config {
	return &vowifi.Config{
		Logger:   mmodem.LoggerForIMEI(imei),
		Terminal: terminalInfo(imei),
		IMS: vowifi.IMSConfig{
			SMSDeliveryReportTimeout: smsDeliveryReportTimeout(),
			Voice:                    browserVoiceConfig(),
		},
	}
}

func terminalInfo(imei string) vowifi.TerminalInfo {
	return vowifi.TerminalInfo{
		ID:              imei,
		Vendor:          terminalVendor,
		Model:           terminalModel,
		SoftwareVersion: terminalSoftwareVersion,
	}
}

func (c *coordinator) watchClient(ctx context.Context, modem *mmodem.Modem, profileID string, sessionID uint64, client *vowifi.Client) {
	events := client.Events()
	defer events.Close()
	smsEvents := client.SMS().Events()
	defer smsEvents.Close()
	voiceEvents := client.Voice().Events()
	defer voiceEvents.Close()
	reconnect := c.reconnectChannel(modem.EquipmentIdentifier, sessionID, client)
	for {
		select {
		case msg, ok := <-smsEvents.Incoming:
			if !ok {
				c.markDisconnected(modem.EquipmentIdentifier, sessionID, client)
				return
			}
			c.forwardIncoming(ctx, modem, profileID, msg)
		case report, ok := <-smsEvents.Reports:
			if !ok {
				c.markDisconnected(modem.EquipmentIdentifier, sessionID, client)
				return
			}
			c.forwardSMSReport(modem.EquipmentIdentifier, profileID, report)
		case incoming, ok := <-voiceEvents.Incoming:
			if !ok {
				c.markDisconnected(modem.EquipmentIdentifier, sessionID, client)
				return
			}
			c.forwardIncomingCall(modem, profileID, sessionID, incoming)
		case event, ok := <-voiceEvents.Events:
			if !ok {
				c.markDisconnected(modem.EquipmentIdentifier, sessionID, client)
				return
			}
			c.forwardCallEvent(modem.EquipmentIdentifier, sessionID, event)
		case state, ok := <-events.State:
			if !ok {
				c.markDisconnected(modem.EquipmentIdentifier, sessionID, client)
				return
			}
			if state.Status == vowifi.StatusFailed || state.Status == vowifi.StatusClosed {
				_ = client.Close()
				c.markDisconnected(modem.EquipmentIdentifier, sessionID, client)
				return
			}
		case <-ctx.Done():
			_ = client.Close()
			c.markDisconnected(modem.EquipmentIdentifier, sessionID, client)
			return
		case <-reconnect:
			_ = client.Close()
			return
		}
	}
}

func (c *coordinator) reconnectChannel(modemID string, sessionID uint64, client *vowifi.Client) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.id != sessionID || session.client != client {
		return nil
	}
	return session.reconnect
}

func (c *coordinator) connectedClient(modemID string, profileID string) (*vowifi.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || !session.connected || session.client == nil || session.profileID != profileID {
		return nil, ErrNotConnected
	}
	return session.client, nil
}

func (c *coordinator) markConnected(modemID string, sessionID uint64, client *vowifi.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if session := c.sessions[modemID]; session != nil && session.id == sessionID {
		session.client = client
		session.connected = true
		session.connectedAt = time.Now()
		session.phase = sessionPhaseConnected
		session.websheet = nil
	}
}

func (c *coordinator) markConnecting(modemID string, sessionID uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if session := c.sessions[modemID]; session != nil && session.id == sessionID {
		session.client = nil
		session.connected = false
		session.connectedAt = time.Time{}
		session.phase = sessionPhaseConnecting
	}
}

func (c *coordinator) markDisconnected(modemID string, sessionID uint64, client *vowifi.Client) {
	c.mu.Lock()
	session := c.sessions[modemID]
	if session == nil || session.id != sessionID || session.client != client {
		c.mu.Unlock()
		return
	}
	session.client = nil
	session.connected = false
	session.connectedAt = time.Time{}
	session.phase = sessionPhaseDisconnected
	events := disconnectedCallEvents(session)
	c.mu.Unlock()

	for _, call := range events {
		c.publishVoiceEvent(call)
	}
}

func (c *coordinator) handleClientDisconnected(modemID string, client *vowifi.Client, err error) error {
	if !errors.Is(err, vowifi.ErrClientNotConnected) {
		return err
	}
	if client != nil {
		c.requestReconnect(modemID, client)
	}
	return ErrNotConnected
}

func (c *coordinator) requestReconnect(modemID string, client *vowifi.Client) {
	c.mu.Lock()
	session := c.sessions[modemID]
	if session == nil || session.client != client {
		c.mu.Unlock()
		return
	}
	ch := session.reconnect
	session.client = nil
	session.connected = false
	session.connectedAt = time.Time{}
	session.phase = sessionPhaseDisconnected
	events := disconnectedCallEvents(session)
	c.mu.Unlock()

	for _, call := range events {
		c.publishVoiceEvent(call)
	}
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func disconnectedCallEvents(session *sessionState) []VoiceCall {
	if session == nil || len(session.calls) == 0 {
		return nil
	}
	now := time.Now()
	events := make([]VoiceCall, 0, len(session.calls))
	for _, state := range session.calls {
		if state == nil || state.info.ID == "" || isTerminalVoiceCallState(state.info.State) {
			continue
		}
		state.info, _ = failVoiceCall(state.info, "wifi calling disconnected", now)
		state.updatedAt = now
		events = append(events, state.info)
	}
	return events
}

func (c *coordinator) stop(modemID string) {
	c.stopSession(modemID, true)
}

func (c *coordinator) stopAsync(modemID string) {
	session, events := c.detachSession(modemID)
	c.closeDetachedSessionAsync(session, events)
}

func (c *coordinator) stopAsyncSession(modemID string, sessionID uint64) {
	session, events := c.detachSessionByID(modemID, sessionID)
	c.closeDetachedSessionAsync(session, events)
}

func (c *coordinator) restart(modem *mmodem.Modem, profileID string) {
	if modem == nil || strings.TrimSpace(modem.EquipmentIdentifier) == "" {
		return
	}
	session, events := c.detachSession(modem.EquipmentIdentifier)
	c.closeDetachedSessionAsync(session, events)
	c.start(modem, profileID)
}

func (c *coordinator) stopSession(modemID string, wait bool) {
	session, events := c.detachSession(modemID)
	c.closeDetachedSession(session, events, wait)
}

func (c *coordinator) closeDetachedSession(session *sessionState, events []VoiceCall, wait bool) {
	if session == nil {
		return
	}
	c.deleteSessionWebsheet(session)
	if session.cancel != nil {
		session.cancel()
	}
	closeSession(session, wait)
	for _, call := range events {
		c.publishVoiceEvent(call)
	}
}

func (c *coordinator) closeDetachedSessionAsync(session *sessionState, events []VoiceCall) {
	if session == nil {
		return
	}
	c.deleteSessionWebsheet(session)
	if session.cancel != nil {
		session.cancel()
	}
	go closeSession(session, true)
	for _, call := range events {
		c.publishVoiceEvent(call)
	}
}

func (c *coordinator) deleteSessionWebsheet(session *sessionState) {
	if session == nil || session.websheet == nil || c.websheets == nil {
		return
	}
	c.websheets.Delete(session.websheet.Info().ID)
	session.websheet = nil
}

func (c *coordinator) detachSession(modemID string) (*sessionState, []VoiceCall) {
	c.mu.Lock()
	session := c.sessions[modemID]
	delete(c.sessions, modemID)
	events := disconnectedCallEvents(session)
	c.mu.Unlock()
	return session, events
}

func (c *coordinator) detachSessionByID(modemID string, sessionID uint64) (*sessionState, []VoiceCall) {
	c.mu.Lock()
	session := c.sessions[modemID]
	if session == nil || session.id != sessionID {
		c.mu.Unlock()
		return nil, nil
	}
	delete(c.sessions, modemID)
	events := disconnectedCallEvents(session)
	c.mu.Unlock()
	return session, events
}

func closeSession(session *sessionState, wait bool) {
	if session == nil {
		return
	}
	if session.client != nil {
		_ = session.client.Close()
	}
	if wait && session.done != nil {
		<-session.done
	}
}

func (c *coordinator) stopAll() {
	c.mu.Lock()
	ids := slices.Collect(maps.Keys(c.sessions))
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
