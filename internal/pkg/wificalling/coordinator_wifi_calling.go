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

	vowifi "github.com/damonto/vowifi-go"
	"github.com/damonto/vowifi-go/driver/at"
	"github.com/damonto/vowifi-go/driver/mbim"
	"github.com/damonto/vowifi-go/driver/qmi"
	imssip "github.com/damonto/vowifi-go/ims/sip"
	imsvoice "github.com/damonto/vowifi-go/ims/voice"
	usimreader "github.com/damonto/vowifi-go/usim/reader"
	"github.com/godbus/dbus/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/websheet"
)

type Config struct {
	Store      *storage.Store
	OnIncoming IncomingSMSFunc
	Websheets  *websheet.Broker
}

type coordinator struct {
	settings   *SettingsStore
	onIncoming IncomingSMSFunc
	websheets  *websheet.Broker

	mu               sync.Mutex
	sessions         map[string]*sessionState
	voiceSubscribers map[uint64]VoiceEventFunc
	nextVoiceSubID   uint64
}

type sessionState struct {
	cancel      context.CancelFunc
	done        <-chan struct{}
	client      *vowifi.Client
	ussd        *vowifi.Session
	calls       map[string]*voiceCallState
	pendingDial *pendingVoiceDial
	modemPath   dbus.ObjectPath
	profileID   string
	connected   bool
	websheet    *websheet.Session
}

type voiceCallState struct {
	call      *imsvoice.Call
	info      VoiceCall
	updatedAt time.Time
}

type pendingVoiceDial struct {
	profileID string
	number    string
	startedAt time.Time
}

var retryDelays = []time.Duration{
	30 * time.Second,
	60 * time.Second,
	120 * time.Second,
	240 * time.Second,
	300 * time.Second,
	600 * time.Second,
}

func New(cfg Config) Coordinator {
	return &coordinator{
		settings:         NewSettingsStore(cfg.Store),
		onIncoming:       cfg.OnIncoming,
		websheets:        cfg.Websheets,
		sessions:         make(map[string]*sessionState),
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
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
	state := StateIdle
	var pending *websheet.Info
	switch {
	case connected:
		state = StateConnected
	case session != nil && session.websheet != nil:
		state = StateWebsheetRequired
		info := session.websheet.Info()
		pending = &info
	case session != nil:
		state = StateConnecting
	case settings.Enabled:
		state = StateDisconnected
	}
	c.mu.Unlock()
	return Status{Settings: settings, Connected: connected, State: state, Websheet: pending}, nil
}

func (c *coordinator) StartWebsheet(ctx context.Context, modem *mmodem.Modem) (websheet.Info, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return websheet.Info{}, err
	}
	c.mu.Lock()
	session := c.sessions[modem.EquipmentIdentifier]
	if session == nil || session.profileID != profileID || session.websheet == nil {
		c.mu.Unlock()
		return websheet.Info{}, ErrWebsheetNotPending
	}
	info := session.websheet.Info()
	c.mu.Unlock()
	return info, nil
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
		ModemID:     modem.EquipmentIdentifier,
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

func (c *coordinator) DialCall(ctx context.Context, modem *mmodem.Modem, to string) (VoiceCall, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return VoiceCall{}, err
	}
	client, err := c.connectedClient(modem.EquipmentIdentifier, profileID)
	if err != nil {
		return VoiceCall{}, err
	}
	pending := c.setPendingVoiceDial(modem.EquipmentIdentifier, profileID, to)
	defer c.clearPendingVoiceDial(modem.EquipmentIdentifier, pending)
	call, err := client.Voice().Dial(ctx, imsvoice.DialRequest{To: to, Media: browserVoiceMediaOffer()})
	if err != nil {
		err = normalizeVoiceError(err)
		if errors.Is(err, ErrNotConnected) {
			return VoiceCall{}, err
		}
		return failedOutgoingVoiceCall(modem.EquipmentIdentifier, profileID, to, err), err
	}
	info := c.storeVoiceCall(modem.EquipmentIdentifier, profileID, call, strings.TrimSpace(to), string(call.Direction()), string(call.State()), "")
	info = initialDialedVoiceCallState(info, call.State())
	if !info.AnsweredAt.IsZero() {
		c.updateVoiceCall(modem.EquipmentIdentifier, info.ID, info)
	}
	c.publishVoiceEvent(info)
	return info, nil
}

func initialDialedVoiceCallState(info VoiceCall, state imsvoice.CallState) VoiceCall {
	if isAnsweredVoiceState(state) && info.AnsweredAt.IsZero() {
		info.AnsweredAt = info.UpdatedAt
	}
	return info
}

func isAnsweredVoiceState(state imsvoice.CallState) bool {
	return state == imsvoice.CallStateActive
}

func failedOutgoingVoiceCall(modemID string, profileID string, to string, err error) VoiceCall {
	return failedOutgoingVoiceCallAt(modemID, profileID, to, err, time.Now())
}

func failedOutgoingVoiceCallAt(modemID string, profileID string, to string, err error, at time.Time) VoiceCall {
	number := strings.TrimSpace(to)
	reason := ""
	if err != nil {
		reason = err.Error()
	}
	return VoiceCall{
		ID:        failedVoiceCallID(modemID, profileID, number, at),
		ModemID:   modemID,
		ProfileID: profileID,
		Direction: string(imsvoice.CallDirectionOutgoing),
		Number:    number,
		State:     string(imsvoice.CallStateFailed),
		Reason:    reason,
		StartedAt: at,
		EndedAt:   at,
		UpdatedAt: at,
	}
}

func failedVoiceCallID(modemID string, profileID string, number string, at time.Time) string {
	sum := sha256.Sum256([]byte(modemID + "\x00" + profileID + "\x00" + number + "\x00" + at.UTC().Format(time.RFC3339Nano)))
	return "failed:" + hex.EncodeToString(sum[:12])
}

func browserVoiceMediaOffer() imsvoice.MediaOffer {
	return imsvoice.MediaOffer{
		Codecs: []imsvoice.AudioCodec{imsvoice.CodecAMR, imsvoice.CodecPCMU},
	}
}

func browserVoiceConfig() imsvoice.Config {
	return imsvoice.Config{
		Codecs: []imsvoice.AudioCodecConfig{
			{
				Name:         imsvoice.CodecAMR,
				PayloadTypes: []int{102},
				ClockRate:    8000,
				ModeSet:      "0,2,4,7",
			},
			{
				Name:         imsvoice.CodecTelephoneEvent,
				PayloadTypes: []int{101},
				ClockRate:    8000,
			},
			{
				Name:         imsvoice.CodecPCMU,
				PayloadTypes: []int{0},
				ClockRate:    8000,
			},
		},
		PTime:    20 * time.Millisecond,
		MaxPTime: 240 * time.Millisecond,
	}
}

func (c *coordinator) AnswerCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	call, info, err := c.lookupVoiceCall(ctx, modem, callID)
	if err != nil {
		return VoiceCall{}, err
	}
	if err := call.Answer(ctx, browserVoiceMediaOffer()); err != nil {
		return VoiceCall{}, normalizeVoiceError(err)
	}
	info.State = string(call.State())
	info.AnsweredAt = time.Now()
	info.UpdatedAt = info.AnsweredAt
	c.updateVoiceCall(modem.EquipmentIdentifier, callID, info)
	c.publishVoiceEvent(info)
	return info, nil
}

func (c *coordinator) RejectCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	call, info, err := c.lookupVoiceCall(ctx, modem, callID)
	if err != nil {
		return VoiceCall{}, err
	}
	if err := call.Reject(ctx, 486, "Busy Here"); err != nil {
		return VoiceCall{}, normalizeVoiceError(err)
	}
	info.State = string(call.State())
	info.Reason = "Busy Here"
	info.EndedAt = time.Now()
	info.UpdatedAt = info.EndedAt
	c.updateVoiceCall(modem.EquipmentIdentifier, callID, info)
	c.publishVoiceEvent(info)
	return info, nil
}

func (c *coordinator) HangupCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	call, info, err := c.lookupVoiceCall(ctx, modem, callID)
	if err != nil {
		return VoiceCall{}, err
	}
	if err := call.Hangup(ctx); err != nil {
		return VoiceCall{}, normalizeVoiceError(err)
	}
	info.State = string(call.State())
	info.EndedAt = time.Now()
	info.UpdatedAt = info.EndedAt
	c.updateVoiceCall(modem.EquipmentIdentifier, callID, info)
	c.publishVoiceEvent(info)
	return info, nil
}

func (c *coordinator) OpenCallMedia(ctx context.Context, modem *mmodem.Modem, callID string) (MediaSession, error) {
	call, _, err := c.lookupVoiceCall(ctx, modem, callID)
	if err != nil {
		return nil, err
	}
	media := call.Media()
	if !isSupportedCallMediaCodec(media.Codec) {
		return nil, ErrUnsupportedCodec
	}
	return callMediaSession{call: call, media: media}, nil
}

func isSupportedCallMediaCodec(codec imsvoice.AudioCodec) bool {
	return codec == imsvoice.CodecAMR || codec == imsvoice.CodecPCMU
}

func (c *coordinator) SubscribeVoiceEvents(fn VoiceEventFunc) func() {
	if fn == nil {
		return func() {}
	}
	c.mu.Lock()
	c.nextVoiceSubID++
	id := c.nextVoiceSubID
	c.voiceSubscribers[id] = fn
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.voiceSubscribers, id)
		c.mu.Unlock()
	}
}

func normalizeVoiceError(err error) error {
	if errors.Is(err, vowifi.ErrClientNotConnected) {
		return ErrNotConnected
	}
	var responseErr *imssip.ResponseError
	if errors.As(err, &responseErr) {
		if text := strings.TrimSpace(responseErr.WarningText()); text != "" {
			return errors.New(text)
		}
		if text := strings.TrimSpace(responseErr.Error()); text != "" {
			return errors.New(text)
		}
	}
	return err
}

type callMediaSession struct {
	call  *imsvoice.Call
	media imsvoice.NegotiatedMedia
}

func (s callMediaSession) Info() MediaInfo {
	return MediaInfo{
		Codec:           string(s.media.Codec),
		PayloadType:     s.media.PayloadType,
		ClockRate:       s.media.ClockRate,
		Channels:        s.media.Channels,
		OctetAlign:      s.media.OctetAlign,
		DTMFPayloadType: s.media.DTMFPayloadType,
		DTMFClockRate:   s.media.DTMFClockRate,
		PTimeMillis:     int(s.media.PTime / time.Millisecond),
	}
}

func (s callMediaSession) ReadPacket(ctx context.Context) ([]byte, error) {
	packet, _, err := s.call.ReadRTP(ctx)
	if err != nil {
		return nil, err
	}
	return packet, nil
}

func (s callMediaSession) WritePacket(ctx context.Context, packet []byte) error {
	return s.call.WriteRTP(ctx, packet)
}

func (c *coordinator) lookupVoiceCall(ctx context.Context, modem *mmodem.Modem, callID string) (*imsvoice.Call, VoiceCall, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return nil, VoiceCall{}, err
	}
	callID = strings.TrimSpace(callID)
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modem.EquipmentIdentifier]
	if session == nil || session.profileID != profileID || session.calls == nil {
		return nil, VoiceCall{}, ErrNotConnected
	}
	state := session.calls[callID]
	if state == nil || state.call == nil {
		return nil, VoiceCall{}, ErrUnavailable
	}
	return state.call, state.info, nil
}

func (c *coordinator) storeVoiceCall(modemID string, profileID string, call *imsvoice.Call, number string, direction string, state string, reason string) VoiceCall {
	now := time.Now()
	info := VoiceCall{
		ID:        call.ID(),
		ModemID:   modemID,
		ProfileID: profileID,
		Direction: direction,
		Number:    number,
		State:     state,
		Reason:    reason,
		StartedAt: now,
		UpdatedAt: now,
	}
	if existing := c.voiceCallInfo(modemID, call.ID()); existing.ID != "" {
		info.StartedAt = existing.StartedAt
		if info.StartedAt.IsZero() {
			info.StartedAt = now
		}
		if !existing.AnsweredAt.IsZero() {
			info.AnsweredAt = existing.AnsweredAt
		}
	}
	c.updateVoiceCallWithPointer(modemID, call.ID(), call, info)
	return info
}

func (c *coordinator) voiceCallInfo(modemID string, callID string) VoiceCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.calls == nil {
		return VoiceCall{}
	}
	state := session.calls[callID]
	if state == nil {
		return VoiceCall{}
	}
	return state.info
}

func (c *coordinator) setPendingVoiceDial(modemID string, profileID string, number string) pendingVoiceDial {
	pending := pendingVoiceDial{
		profileID: profileID,
		number:    strings.TrimSpace(number),
		startedAt: time.Now(),
	}
	c.mu.Lock()
	if session := c.sessions[modemID]; session != nil {
		session.pendingDial = &pending
	}
	c.mu.Unlock()
	return pending
}

func (c *coordinator) clearPendingVoiceDial(modemID string, pending pendingVoiceDial) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.pendingDial == nil {
		return
	}
	if *session.pendingDial == pending {
		session.pendingDial = nil
	}
}

func (c *coordinator) updateVoiceCall(modemID string, callID string, info VoiceCall) {
	c.updateVoiceCallWithPointer(modemID, callID, nil, info)
}

func (c *coordinator) updateVoiceCallWithPointer(modemID string, callID string, call *imsvoice.Call, info VoiceCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil {
		return
	}
	if session.calls == nil {
		session.calls = make(map[string]*voiceCallState)
	}
	state := session.calls[callID]
	if state == nil {
		state = &voiceCallState{}
		session.calls[callID] = state
	}
	if call != nil {
		state.call = call
	}
	state.info = info
	state.updatedAt = info.UpdatedAt
}

func (c *coordinator) publishVoiceEvent(call VoiceCall) {
	c.mu.Lock()
	subscribers := make([]VoiceEventFunc, 0, len(c.voiceSubscribers))
	for _, fn := range c.voiceSubscribers {
		subscribers = append(subscribers, fn)
	}
	c.mu.Unlock()
	event := VoiceEvent{Call: call}
	for _, fn := range subscribers {
		fn(event)
	}
}

func (c *coordinator) ussdSession(modemID string) (*vowifi.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.ussd == nil {
		return nil, vowifi.ErrUSSDNotStarted
	}
	return session.ussd, nil
}

func (c *coordinator) setUSSDSession(modemID string, ussd *vowifi.Session, closed bool) {
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
	done := make(chan struct{})
	c.sessions[modemID] = &sessionState{
		cancel:    cancel,
		done:      done,
		modemPath: modem.Path(),
		profileID: profileID,
		calls:     make(map[string]*voiceCallState),
	}
	c.mu.Unlock()
	go func() {
		defer close(done)
		c.connectLoop(ctx, modem, profileID)
	}()
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
		if errors.Is(err, vowifi.ErrWFCEntitlementUserActionRequired) {
			slog.Warn("Wi-Fi Calling requires carrier websheet", "modem", modem.EquipmentIdentifier, "error", err)
			if err := c.waitForWebsheet(ctx, modem.EquipmentIdentifier); err != nil {
				if errors.Is(err, ErrWebsheetDismissed) {
					slog.Info("Wi-Fi Calling carrier websheet dismissed", "modem", modem.EquipmentIdentifier)
					c.stopAsync(modem.EquipmentIdentifier)
				}
				return
			}
			attempt = -1
			continue
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

func (c *coordinator) connectOnce(ctx context.Context, modem *mmodem.Modem) (*vowifi.Client, error) {
	reader, err := openReader(ctx, modem)
	if err != nil {
		return nil, err
	}
	client, err := vowifi.New(reader, &vowifi.Config{
		IMEI:   modem.EquipmentIdentifier,
		Logger: slog.Default(),
		IMS: vowifi.IMSConfig{
			Voice: browserVoiceConfig(),
		},
	})
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
			c.setWebsheet(modem.EquipmentIdentifier, session)
		}
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *coordinator) wfcWebsheetRequest(err error) (websheet.Request, bool) {
	if c.websheets == nil || !errors.Is(err, vowifi.ErrWFCEntitlementUserActionRequired) {
		return websheet.Request{}, false
	}
	var nsdsErr *vowifi.NSDSWFCEntitlementError
	if errors.As(err, &nsdsErr) {
		return websheet.Request{
			URL:         strings.TrimSpace(nsdsErr.Websheet.URL),
			UserData:    wfcUserActionData(nsdsErr.Websheet.Data),
			ContentType: "application/x-www-form-urlencoded",
			Title:       firstNonEmpty(nsdsErr.Websheet.Title, "Wi-Fi Calling"),
		}, true
	}
	var ts43Err *vowifi.WFCEntitlementError
	if errors.As(err, &ts43Err) {
		return websheet.Request{
			URL:   wfcUserActionURL(ts43Err.Status.ServiceFlowURL, ts43Err.Status.ServiceFlowUserData),
			Title: firstNonEmpty(ts43Err.Config.CarrierName, "Wi-Fi Calling"),
		}, true
	}
	return websheet.Request{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (c *coordinator) setWebsheet(modemID string, websheetSession *websheet.Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if session := c.sessions[modemID]; session != nil {
		session.websheet = websheetSession
	}
}

func (c *coordinator) waitForWebsheet(ctx context.Context, modemID string) error {
	c.mu.Lock()
	session := c.sessions[modemID]
	var websheetSession *websheet.Session
	if session != nil {
		websheetSession = session.websheet
	}
	c.mu.Unlock()
	if websheetSession == nil {
		return ErrWebsheetNotPending
	}
	for {
		callback, err := websheetSession.WaitCallback(ctx)
		if err != nil {
			return err
		}
		switch wfcWebsheetCallbackResult(callback) {
		case wfcWebsheetCallbackRetry:
			c.clearWebsheet(modemID, websheetSession)
			return nil
		case wfcWebsheetCallbackDismiss:
			c.clearWebsheet(modemID, websheetSession)
			return ErrWebsheetDismissed
		}
	}
}

func (c *coordinator) clearWebsheet(modemID string, websheetSession *websheet.Session) {
	c.mu.Lock()
	if session := c.sessions[modemID]; session != nil && session.websheet == websheetSession {
		session.websheet = nil
	}
	c.mu.Unlock()
	if c.websheets != nil {
		c.websheets.Delete(websheetSession.Info().ID)
	}
}

type wfcWebsheetCallbackAction int

const (
	wfcWebsheetCallbackWait wfcWebsheetCallbackAction = iota
	wfcWebsheetCallbackRetry
	wfcWebsheetCallbackDismiss
)

func wfcWebsheetCallbackResult(callback websheet.Callback) wfcWebsheetCallbackAction {
	event := normalizeWebsheetCallbackKey(firstNonEmpty(callback.Event, callback.Method, callback.ResultCode))
	method := normalizeWebsheetCallbackKey(callback.Method)
	result := normalizeWebsheetCallbackKey(callback.ResultCode)
	switch {
	case event == "dismissflow" || event == "cancel" || result == "cancel":
		return wfcWebsheetCallbackDismiss
	case strings.Contains(method, "cancel") || strings.Contains(method, "closewebview"):
		return wfcWebsheetCallbackDismiss
	case event == "entitlementchanged" || event == "finishflow" || event == "done" || event == "phoneservicesaccountstatuschanged" || result == "success":
		return wfcWebsheetCallbackRetry
	default:
		return wfcWebsheetCallbackWait
	}
}

func normalizeWebsheetCallbackKey(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (c *coordinator) watchClient(ctx context.Context, modem *mmodem.Modem, profileID string, client *vowifi.Client) {
	events := client.Events()
	defer events.Close()
	smsEvents := client.SMS().Events()
	defer smsEvents.Close()
	voiceEvents := client.Voice().Events()
	defer voiceEvents.Close()
	for {
		select {
		case msg, ok := <-smsEvents.Incoming:
			if !ok {
				c.markDisconnected(modem.EquipmentIdentifier, client)
				return
			}
			c.forwardIncoming(ctx, modem, profileID, msg)
		case incoming, ok := <-voiceEvents.Incoming:
			if !ok {
				c.markDisconnected(modem.EquipmentIdentifier, client)
				return
			}
			c.forwardIncomingCall(modem, profileID, incoming)
		case event, ok := <-voiceEvents.Events:
			if !ok {
				c.markDisconnected(modem.EquipmentIdentifier, client)
				return
			}
			c.forwardCallEvent(modem.EquipmentIdentifier, event)
		case state, ok := <-events.State:
			if !ok {
				c.markDisconnected(modem.EquipmentIdentifier, client)
				return
			}
			if state == vowifi.StateFailed || state == vowifi.StateClosed {
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

func (c *coordinator) forwardIncomingCall(modem *mmodem.Modem, profileID string, incoming vowifi.IncomingCall) {
	if incoming.Call == nil {
		return
	}
	info := c.storeVoiceCall(modem.EquipmentIdentifier, profileID, incoming.Call, incoming.FromNumber, string(incoming.Call.Direction()), string(incoming.Call.State()), "")
	if !incoming.ReceivedAt.IsZero() {
		info.StartedAt = incoming.ReceivedAt
		info.UpdatedAt = incoming.ReceivedAt
		c.updateVoiceCall(modem.EquipmentIdentifier, info.ID, info)
	}
	c.publishVoiceEvent(info)
}

func (c *coordinator) forwardCallEvent(modemID string, event vowifi.CallEvent) {
	c.mu.Lock()
	session := c.sessions[modemID]
	var info VoiceCall
	if session != nil && session.calls != nil {
		state := session.calls[event.CallID]
		if state == nil && session.pendingDial != nil {
			info = VoiceCall{
				ID:        event.CallID,
				ModemID:   modemID,
				ProfileID: session.pendingDial.profileID,
				Direction: string(imsvoice.CallDirectionOutgoing),
				Number:    session.pendingDial.number,
				StartedAt: session.pendingDial.startedAt,
			}
			state = &voiceCallState{info: info, updatedAt: info.StartedAt}
			session.calls[event.CallID] = state
		}
		if state != nil {
			if event.Call != nil {
				state.call = event.Call
			}
			info = state.info
			info.State = string(event.State)
			info.Reason = event.Cause
			info.UpdatedAt = event.At
			if info.UpdatedAt.IsZero() {
				info.UpdatedAt = time.Now()
			}
			if isAnsweredVoiceState(event.State) && info.AnsweredAt.IsZero() {
				info.AnsweredAt = info.UpdatedAt
			}
			if event.State == imsvoice.CallStateEnded || event.State == imsvoice.CallStateFailed {
				info.EndedAt = info.UpdatedAt
			}
			state.info = info
			state.updatedAt = info.UpdatedAt
		}
	}
	c.mu.Unlock()
	if info.ID != "" {
		c.publishVoiceEvent(info)
	}
}

func (c *coordinator) forwardIncoming(ctx context.Context, modem *mmodem.Modem, profileID string, msg vowifi.SMS) {
	if c.onIncoming == nil {
		return
	}
	stored := storage.Message{
		ModemID:     modem.EquipmentIdentifier,
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

func (c *coordinator) connectedClient(modemID string, profileID string) (*vowifi.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || !session.connected || session.client == nil || session.profileID != profileID {
		return nil, ErrNotConnected
	}
	return session.client, nil
}

func (c *coordinator) markConnected(modemID string, client *vowifi.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if session := c.sessions[modemID]; session != nil {
		session.client = client
		session.connected = true
	}
}

func (c *coordinator) markDisconnected(modemID string, client *vowifi.Client) {
	c.mu.Lock()
	session := c.sessions[modemID]
	if session == nil || session.client != client {
		c.mu.Unlock()
		return
	}
	session.client = nil
	session.connected = false
	events := disconnectedCallEvents(session)
	c.mu.Unlock()

	for _, call := range events {
		c.publishVoiceEvent(call)
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
		state.info.State = string(imsvoice.CallStateFailed)
		state.info.Reason = "wifi calling disconnected"
		state.info.EndedAt = now
		state.info.UpdatedAt = now
		state.updatedAt = now
		events = append(events, state.info)
	}
	return events
}

func isTerminalVoiceCallState(state string) bool {
	return state == string(imsvoice.CallStateEnded) || state == string(imsvoice.CallStateFailed)
}

func (c *coordinator) stop(modemID string) {
	c.stopSession(modemID, true)
}

func (c *coordinator) stopAsync(modemID string) {
	c.stopSession(modemID, false)
}

func (c *coordinator) stopSession(modemID string, wait bool) {
	c.mu.Lock()
	session := c.sessions[modemID]
	delete(c.sessions, modemID)
	events := disconnectedCallEvents(session)
	c.mu.Unlock()
	if session == nil {
		return
	}
	if session.cancel != nil {
		session.cancel()
	}
	if session.client != nil {
		_ = session.client.Close()
	}
	if wait && session.done != nil {
		<-session.done
	}
	for _, call := range events {
		c.publishVoiceEvent(call)
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
	return openReaderWith(ctx, modem, openReaderCandidate)
}

type readerCandidate struct {
	portType mmodem.ModemPortType
	device   string
}

type readerOpener func(context.Context, readerCandidate, int) (usimreader.Reader, error)

func openReaderWith(ctx context.Context, modem *mmodem.Modem, open readerOpener) (usimreader.Reader, error) {
	slot := 1
	if modem.PrimarySimSlot > 0 {
		slot = int(modem.PrimarySimSlot)
	}
	candidates := readerCandidates(modem)
	if len(candidates) == 0 {
		return nil, errors.New("Wi-Fi Calling requires QMI, MBIM, or AT modem port")
	}
	var result error
	for _, candidate := range candidates {
		reader, err := open(ctx, candidate, slot)
		if err == nil {
			return reader, nil
		}
		result = errors.Join(result, fmt.Errorf("open %s reader on %s: %w", readerPortTypeName(candidate.portType), candidate.device, err))
	}
	return nil, result
}

func readerCandidates(modem *mmodem.Modem) []readerCandidate {
	if modem == nil {
		return nil
	}
	var candidates []readerCandidate
	add := func(portType mmodem.ModemPortType, device string) {
		device = strings.TrimSpace(device)
		if device == "" || !supportedReaderPort(portType) {
			return
		}
		for _, candidate := range candidates {
			if candidate.portType == portType && candidate.device == device {
				return
			}
		}
		candidates = append(candidates, readerCandidate{portType: portType, device: device})
	}
	add(modem.PrimaryPortType(), modem.PrimaryPort)
	for _, portType := range []mmodem.ModemPortType{mmodem.ModemPortTypeQmi, mmodem.ModemPortTypeMbim, mmodem.ModemPortTypeAt} {
		for _, port := range modem.Ports {
			if port.PortType == portType {
				add(portType, port.Device)
			}
		}
	}
	return candidates
}

func supportedReaderPort(portType mmodem.ModemPortType) bool {
	return portType == mmodem.ModemPortTypeQmi || portType == mmodem.ModemPortTypeMbim || portType == mmodem.ModemPortTypeAt
}

func openReaderCandidate(ctx context.Context, candidate readerCandidate, slot int) (usimreader.Reader, error) {
	switch candidate.portType {
	case mmodem.ModemPortTypeQmi:
		return qmi.Open(ctx, candidate.device, slot)
	case mmodem.ModemPortTypeMbim:
		return mbim.Open(ctx, candidate.device, slot)
	case mmodem.ModemPortTypeAt:
		return at.New(candidate.device, 0)
	default:
		return nil, errors.New("reader port type is unsupported")
	}
}

func readerPortTypeName(portType mmodem.ModemPortType) string {
	switch portType {
	case mmodem.ModemPortTypeQmi:
		return "QMI"
	case mmodem.ModemPortTypeMbim:
		return "MBIM"
	case mmodem.ModemPortTypeAt:
		return "AT"
	default:
		return "unknown"
	}
}

func incomingMessageKey(msg vowifi.SMS) string {
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
