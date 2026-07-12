//go:build ims

package ims

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	imsgo "github.com/damonto/ims-go"
	"github.com/damonto/ims-go/lte"
	"github.com/damonto/ims-go/wfcsetup"
	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
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
	voLTERestoreTimeout     = 5 * time.Second
)

var (
	voLTEResetDelay           = time.Second
	packetServicePollInterval = time.Second
	packetServiceWaitTimeout  = time.Minute
	internetRestoreInterval   = 2 * time.Second
	internetRestoreTimeout    = time.Minute
)

type managedVoLTEDevice interface {
	Close() error
	VoLTEStatus(ctx context.Context) (wwan.VoLTEStatus, error)
	PacketServiceStatus(ctx context.Context) (wwan.PacketServiceStatus, error)
	IMSProfileIndex(ctx context.Context) (uint8, error)
	IMSSTestMode(ctx context.Context) (bool, error)
	SetIMSSTestMode(ctx context.Context, enabled bool) error
	SetAirplaneMode(ctx context.Context, enabled bool) error
}

type internetRestorer interface {
	Current(ctx context.Context, modem *mmodem.Modem) (*pinternet.Connection, error)
	Connect(ctx context.Context, modem *mmodem.Modem, prefs pinternet.Preferences) (*pinternet.Connection, error)
}

var openManagedVoLTEDevice = func(modem *mmodem.Modem) (managedVoLTEDevice, error) {
	return mmodem.OpenVoLTEStatusDevice(modem)
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
		slog.Debug("skip IMS start", "imei", modem.EquipmentIdentifier, "access", c.routeName(), "error", err)
		return
	}
	settings, err := c.Settings(ctx, modem)
	if err != nil {
		slog.Warn("read IMS settings", "imei", modem.EquipmentIdentifier, "access", c.routeName(), "error", err)
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
		modem:     modem,
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
		slog.Warn("IMS access disconnected", "imei", modem.EquipmentIdentifier, "access", c.routeName(), "retryIn", delay)
		if err := sleep(ctx, delay); err != nil {
			return
		}
	}
}

func (c *coordinator) connectWithRetry(ctx context.Context, modem *mmodem.Modem, sessionID uint64) (*imsgo.Client, error) {
	attempt := 0
	for {
		client, err := c.connectOnce(ctx, modem, sessionID)
		if err == nil {
			return client, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, ErrUnavailable) {
			slog.Warn("IMS access unavailable", "imei", modem.EquipmentIdentifier, "access", c.routeName(), "error", err)
			return nil, err
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
			slog.Warn("IMS access connection attempts exhausted", "imei", modem.EquipmentIdentifier, "access", c.routeName(), "error", err)
			return nil, err
		}
		delay := retryDelays[attempt]
		attempt++
		slog.Warn("IMS access connect", "imei", modem.EquipmentIdentifier, "access", c.routeName(), "retryIn", delay, "error", err)
		if err := sleep(ctx, delay); err != nil {
			return nil, err
		}
	}
}

func (c *coordinator) connectOnce(ctx context.Context, modem *mmodem.Modem, sessionID uint64) (*imsgo.Client, error) {
	var imsProfileIndex uint8
	var volteInterfaceName string
	var preparedQMAP mmodem.PreparedQMAP
	if c.access == AccessVoLTE {
		var err error
		imsProfileIndex, err = prepareManagedVoLTE(ctx, modem, c.internet)
		if err != nil {
			return nil, err
		}
		port, err := voLTEControlPort(modem)
		if err != nil {
			return nil, err
		}
		if port.PortType == mmodem.ModemPortTypeQmi {
			preparedQMAP, err = mmodem.PrepareQMAP(ctx, modem, 2)
			if err != nil {
				return nil, fmt.Errorf("prepare IMS QMAP mux 2: %w", err)
			}
			volteInterfaceName = preparedQMAP.InterfaceName
		} else {
			volteInterfaceName, err = voLTEInterfaceName(modem)
			if err != nil {
				return nil, err
			}
		}
	}
	cfg, err := c.modemClientConfig(ctx, modem, imsProfileIndex, volteInterfaceName)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		reader, err := OpenWWAN(ctx, modem, WWANConfig{Access: c.access, QMAP: preparedQMAP})
		if err != nil {
			return nil, err
		}
		client, err := imsgo.New(reader, cfg)
		if err != nil {
			return nil, err
		}
		if err := client.Connect(ctx); err != nil {
			_ = client.Close()
			if c.access == AccessVoLTE && attempt == 0 && isIMSCallAlreadyPresent(err) {
				if resetErr := resetOccupiedManagedVoLTE(ctx, modem, c.internet); resetErr != nil {
					return nil, errors.Join(err, fmt.Errorf("reset occupied IMS PDN: %w", resetErr))
				}
				continue
			}
			if req, ok := c.wfcWebsheetRequest(err); ok {
				session, serr := c.websheets.Create(ctx, req)
				if serr != nil {
					return nil, errors.Join(err, serr)
				}
				c.attachWebsheet(modem.EquipmentIdentifier, sessionID, session)
			}
			return nil, err
		}
		return client, nil
	}
	return nil, errors.New("connect IMS after modem reset")
}

func (c *coordinator) modemClientConfig(ctx context.Context, modem *mmodem.Modem, imsProfileIndex uint8, volteInterfaceName string) (*imsgo.Config, error) {
	imei, err := modem.ThreeGPP().IMEI(ctx)
	if err != nil {
		return nil, fmt.Errorf("read modem IMEI: %w", err)
	}
	cfg := modemClientConfigForIMEI(imei, c.access, imsProfileIndex)
	if c.access == AccessVoLTE {
		cfg.Access.VoLTE.InterfaceName = volteInterfaceName
	}
	return cfg, nil
}

func wifiCallingModemClientConfig(ctx context.Context, modem *mmodem.Modem) (*imsgo.Config, error) {
	imei, err := modem.ThreeGPP().IMEI(ctx)
	if err != nil {
		return nil, fmt.Errorf("read modem IMEI: %w", err)
	}
	return modemClientConfigForIMEI(imei, AccessWiFiCalling, 0), nil
}

func modemClientConfigForIMEI(imei string, access Access, imsProfileIndex uint8) *imsgo.Config {
	accessConfig := imsgo.VoWiFi(imsgo.VoWiFiConfig{})
	if access == AccessVoLTE {
		accessConfig = imsgo.VoLTE(lte.Config{
			APN:          lte.DefaultAPN,
			ProfileIndex: imsProfileIndex,
		})
	}
	return &imsgo.Config{
		Logger:   mmodem.LoggerForIMEI(imei),
		Terminal: terminalInfo(imei),
		Access:   accessConfig,
		IMS: imsgo.IMSConfig{
			SMSDeliveryReportTimeout: smsDeliveryReportTimeout(),
			Voice:                    browserVoiceConfig(),
		},
	}
}

func prepareManagedVoLTE(ctx context.Context, modem *mmodem.Modem, internet internetRestorer) (profileIndex uint8, err error) {
	device, err := openManagedVoLTEDevice(modem)
	if errors.Is(err, wwan.ErrUnsupported) {
		return 0, ErrUnavailable
	}
	if err != nil {
		return 0, fmt.Errorf("open device: %w", err)
	}
	defer func() {
		err = errors.Join(err, device.Close())
	}()
	status, err := device.VoLTEStatus(ctx)
	if err != nil {
		return 0, fmt.Errorf("read volte status: %w", err)
	}
	if !status.Supported {
		return 0, ErrUnavailable
	}
	profileIndex, err = device.IMSProfileIndex(ctx)
	if err != nil {
		return 0, fmt.Errorf("find IMS profile: %w", err)
	}
	packetServiceReady := false
	if status.Occupied {
		testMode, err := device.IMSSTestMode(ctx)
		if err != nil {
			return 0, fmt.Errorf("read IMSS test mode: %w", err)
		}
		if !testMode {
			if err := device.SetIMSSTestMode(ctx, true); err != nil {
				return 0, fmt.Errorf("enable IMSS test mode: %w", err)
			}
			if err := resetManagedVoLTE(ctx, modem, device, internet); err != nil {
				return 0, err
			}
			packetServiceReady = true
		}
	}
	if !packetServiceReady {
		waitCtx, cancel := context.WithTimeout(ctx, packetServiceWaitTimeout)
		err := waitForPacketService(waitCtx, device)
		cancel()
		if err != nil {
			return 0, err
		}
	}
	return profileIndex, nil
}

func releaseManagedVoLTE(ctx context.Context, modem *mmodem.Modem, internet internetRestorer) (err error) {
	device, err := openManagedVoLTEDevice(modem)
	if errors.Is(err, wwan.ErrUnsupported) {
		return ErrUnavailable
	}
	if err != nil {
		return fmt.Errorf("open device: %w", err)
	}
	defer func() {
		err = errors.Join(err, device.Close())
	}()
	testMode, err := device.IMSSTestMode(ctx)
	if errors.Is(err, wwan.ErrUnsupported) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read IMSS test mode: %w", err)
	}
	if !testMode {
		return nil
	}
	if err := device.SetIMSSTestMode(ctx, false); err != nil {
		return fmt.Errorf("disable IMSS test mode: %w", err)
	}
	return resetManagedVoLTE(ctx, modem, device, internet)
}

func resetManagedVoLTE(ctx context.Context, modem *mmodem.Modem, device managedVoLTEDevice, internet internetRestorer) error {
	prefs, reconnect, err := internetBeforeVoLTEReset(ctx, modem, internet)
	if err != nil {
		return err
	}
	if err := cycleVoLTEAirplaneMode(ctx, device); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), packetServiceWaitTimeout)
	waitErr := waitForPacketService(waitCtx, device)
	cancel()
	var internetErr error
	if reconnect {
		internetErr = restoreInternetAfterVoLTEReset(context.WithoutCancel(ctx), modem, internet, prefs)
	}
	return errors.Join(waitErr, internetErr)
}

func resetOccupiedManagedVoLTE(ctx context.Context, modem *mmodem.Modem, internet internetRestorer) (err error) {
	device, err := openManagedVoLTEDevice(modem)
	if err != nil {
		return fmt.Errorf("open device: %w", err)
	}
	defer func() {
		err = errors.Join(err, device.Close())
	}()
	return resetManagedVoLTE(ctx, modem, device, internet)
}

func restoreInternetAfterVoLTEReset(ctx context.Context, modem *mmodem.Modem, internet internetRestorer, prefs pinternet.Preferences) error {
	restoreCtx, cancel := context.WithTimeout(ctx, internetRestoreTimeout)
	defer cancel()
	ticker := time.NewTicker(internetRestoreInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		if _, err := internet.Connect(restoreCtx, modem, prefs); err == nil {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-restoreCtx.Done():
			return errors.Join(fmt.Errorf("restore internet after IMS reset: %w", lastErr), restoreCtx.Err())
		case <-ticker.C:
		}
	}
}

func internetBeforeVoLTEReset(ctx context.Context, modem *mmodem.Modem, internet internetRestorer) (pinternet.Preferences, bool, error) {
	if internet == nil {
		return pinternet.Preferences{}, false, nil
	}
	connection, err := internet.Current(ctx, modem)
	if err != nil {
		return pinternet.Preferences{}, false, fmt.Errorf("read internet before IMS reset: %w", err)
	}
	if connection == nil || connection.Status != pinternet.StatusConnected {
		return pinternet.Preferences{}, false, nil
	}
	return pinternet.Preferences{
		APN:          connection.APN,
		IPType:       connection.IPType,
		APNUsername:  connection.APNUsername,
		APNPassword:  connection.APNPassword,
		APNAuth:      connection.APNAuth,
		DefaultRoute: connection.DefaultRoute,
		ProxyEnabled: connection.ProxyEnabled,
		AlwaysOn:     connection.AlwaysOn,
	}, true, nil
}

func cycleVoLTEAirplaneMode(ctx context.Context, device managedVoLTEDevice) error {
	if err := device.SetAirplaneMode(ctx, true); err != nil {
		return fmt.Errorf("enable airplane mode: %w", err)
	}
	if err := sleep(ctx, voLTEResetDelay); err != nil {
		return errors.Join(fmt.Errorf("wait for IMS reset: %w", err), restoreVoLTEOnline(ctx, device))
	}
	if err := restoreVoLTEOnline(ctx, device); err != nil {
		return err
	}
	return nil
}

func waitForPacketService(ctx context.Context, device managedVoLTEDevice) error {
	ticker := time.NewTicker(packetServicePollInterval)
	defer ticker.Stop()

	for {
		status, err := device.PacketServiceStatus(ctx)
		if err == nil && status.Registered && status.PSAttached && status.LTE {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("packet service unavailable: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func restoreVoLTEOnline(ctx context.Context, device managedVoLTEDevice) error {
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), voLTERestoreTimeout)
	defer cancel()
	if err := device.SetAirplaneMode(restoreCtx, false); err != nil {
		return fmt.Errorf("disable airplane mode: %w", err)
	}
	return nil
}

func terminalInfo(imei string) imsgo.TerminalInfo {
	return imsgo.TerminalInfo{
		ID:              imei,
		Vendor:          terminalVendor,
		Model:           terminalModel,
		SoftwareVersion: terminalSoftwareVersion,
	}
}

func (c *coordinator) watchClient(ctx context.Context, modem *mmodem.Modem, profileID string, sessionID uint64, client *imsgo.Client) {
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
			if state.Status == imsgo.StatusFailed || state.Status == imsgo.StatusClosed {
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

func (c *coordinator) reconnectChannel(modemID string, sessionID uint64, client *imsgo.Client) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.id != sessionID || session.client != client {
		return nil
	}
	return session.reconnect
}

func (c *coordinator) connectedClient(modemID string, profileID string) (*imsgo.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || !session.connected || session.client == nil || session.profileID != profileID {
		return nil, ErrNotConnected
	}
	return session.client, nil
}

func (c *coordinator) markConnected(modemID string, sessionID uint64, client *imsgo.Client) {
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

func (c *coordinator) markDisconnected(modemID string, sessionID uint64, client *imsgo.Client) {
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
	events := c.disconnectedCallEvents(session)
	c.mu.Unlock()

	for _, call := range events {
		c.publishVoiceEvent(call)
	}
}

func (c *coordinator) handleClientDisconnected(modemID string, client *imsgo.Client, err error) error {
	if !errors.Is(err, imsgo.ErrClientNotConnected) {
		return err
	}
	if client != nil {
		c.requestReconnect(modemID, client)
	}
	return ErrNotConnected
}

func (c *coordinator) requestReconnect(modemID string, client *imsgo.Client) {
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
	events := c.disconnectedCallEvents(session)
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

func (c *coordinator) disconnectedCallEvents(session *sessionState) []VoiceCall {
	if session == nil || len(session.calls) == 0 {
		return nil
	}
	now := time.Now()
	events := make([]VoiceCall, 0, len(session.calls))
	for _, state := range session.calls {
		if state == nil || state.info.ID == "" || isTerminalVoiceCallState(state.info.State) {
			continue
		}
		state.info, _ = failVoiceCall(state.info, c.disconnectedReason(), now)
		state.updatedAt = now
		events = append(events, state.info)
	}
	return events
}

func (c *coordinator) disconnectedReason() string {
	if c.access == AccessVoLTE {
		return "volte disconnected"
	}
	return "wifi calling disconnected"
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
	events := c.disconnectedCallEvents(session)
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
	events := c.disconnectedCallEvents(session)
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

func (c *coordinator) stopAll() []*mmodem.Modem {
	c.mu.Lock()
	ids := slices.Collect(maps.Keys(c.sessions))
	modems := make([]*mmodem.Modem, 0, len(ids))
	for _, modemID := range ids {
		if session := c.sessions[modemID]; session != nil && session.modem != nil {
			modems = append(modems, session.modem)
		}
	}
	c.mu.Unlock()
	for _, modemID := range ids {
		c.stop(modemID)
	}
	return modems
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
