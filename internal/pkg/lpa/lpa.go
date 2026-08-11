package lpa

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/damonto/euicc-go/bertlv"
	"github.com/damonto/euicc-go/bertlv/primitive"
	"github.com/damonto/euicc-go/driver"
	"github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/euicc"
	"github.com/damonto/sigmo/internal/pkg/keymutex"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

const channelOpenTimeout = 30 * time.Second

// Qualcomm UIM APDU commands address logical slots. Sigmo binds the selected
// physical card to the module's primary logical slot before opening LPA.
const qmiPrimaryLogicalSlot uint8 = 1

// operationLocks serializes LPA operations for the same modem or external reader. This is necessary
// because eUICC operations cannot safely share one underlying smart-card channel.
var operationLocks = keymutex.New()

// Client owns one connected eUICC client and its underlying smart-card
// transport. Pool callers receive a Lease instead, so Close always means
// releasing the resource owned by Client.
type Client struct {
	*clientView
	lockKey        string
	releaseSIMSlot func()
	channel        driver.SmartCardChannel
	logger         *slog.Logger
	operation      *operationContext
	shutdown       func() error
	closeOnce      sync.Once
	closeErr       error
}

type Info struct {
	EID          string
	FreeSpace    int32
	SASUP        euicc.SASUP
	Certificates []string
}

type ChannelConfig struct {
	LockKey  string
	ConfigID string
	AID      []byte
	Settings *settings.Settings
	Logger   *slog.Logger
}

// ChannelFactory creates a fresh smart-card channel for one LPA initialization
// attempt. The caller transfers the returned channel to euicc-go's LPA client.
type ChannelFactory func(context.Context) (driver.SmartCardChannel, error)

var (
	ErrNoSupportedAID          = errors.New("no supported ISD-R AID found or it's not an eUICC")
	errAIDNotSupported         = errors.New("AID is not supported")
	errCacheableNoSupportedAID = errors.New("all ISD-R supportedAIDs are unsupported")
)

var supportedAIDs = [][]byte{
	lpa.GSMAISDRApplicationAID,
	{0xA0, 0x00, 0x00, 0x05, 0x59, 0x10, 0x10, 0xFF, 0xFF, 0xFF, 0xFF, 0x89, 0x00, 0x05, 0x05, 0x00}, // 5ber Ultra
	{0xA0, 0x00, 0x00, 0x05, 0x59, 0x10, 0x10, 0x00, 0x00, 0x00, 0x89, 0x00, 0x00, 0x00, 0x03, 0x00}, // eSIM.me V2
	{0xA0, 0x65, 0x73, 0x74, 0x6B, 0x6D, 0x65, 0xFF, 0xFF, 0xFF, 0xFF, 0x49, 0x53, 0x44, 0x2D, 0x52}, // ESTKme 2025
	{0xA0, 0x00, 0x00, 0x05, 0x59, 0x10, 0x10, 0xFF, 0xFF, 0xFF, 0xFF, 0x89, 0x00, 0x00, 0x01, 0x77}, // XeSIM
	{0xA0, 0x00, 0x00, 0x06, 0x28, 0x10, 0x10, 0xFF, 0xFF, 0xFF, 0xFF, 0x89, 0x00, 0x00, 0x01, 0x00}, // GlocalMe
}

func New(ctx context.Context, m *modem.Modem, currentSettings *settings.Settings) (*Client, error) {
	return newModemClient(ctx, modemClientConfig{modem: m, settings: currentSettings})
}

func NewWithAID(ctx context.Context, m *modem.Modem, currentSettings *settings.Settings, aid []byte) (*Client, error) {
	return newModemClient(ctx, modemClientConfig{modem: m, settings: currentSettings, aid: aid})
}

type modemClientConfig struct {
	modem    *modem.Modem
	settings *settings.Settings
	slot     uint8
	aid      []byte
}

func newModemClient(ctx context.Context, cfg modemClientConfig) (*Client, error) {
	if cfg.modem == nil {
		return nil, errors.New("modem is required")
	}
	releaseSIMSlot, err := cfg.modem.ReserveSIMSlot(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve LPA SIM slot: %w", err)
	}
	cfg.slot, err = modem.ActiveSIMSlot(cfg.modem)
	if err != nil {
		releaseSIMSlot()
		return nil, err
	}
	lockKey := lpaLockKey(cfg.modem, cfg.slot)
	if err := operationLocks.LockContext(ctx, lockKey); err != nil {
		releaseSIMSlot()
		return nil, fmt.Errorf("reserve LPA client: %w", err)
	}
	instance, err := newClientForSlot(ctx, cfg)
	if err != nil {
		operationLocks.Unlock(lockKey)
		releaseSIMSlot()
		return nil, err
	}
	instance.lockKey = lockKey
	instance.releaseSIMSlot = releaseSIMSlot
	return instance, nil
}

// newClientForSlot creates a client while the caller owns the modem's SIM slot
// reservation and the corresponding LPA lock.
func newClientForSlot(ctx context.Context, cfg modemClientConfig) (*Client, error) {
	return newWithChannelFactoryLocked(ctx, ChannelConfig{
		ConfigID: cfg.modem.EquipmentIdentifier,
		AID:      cfg.aid,
		Settings: cfg.settings,
		Logger:   cfg.modem.Logger(),
	}, func(ctx context.Context) (driver.SmartCardChannel, error) {
		return createChannelForSlot(ctx, cfg.modem, cfg.slot)
	})
}

func lpaLockKey(m *modem.Modem, slot uint8) string {
	if portType := m.PrimaryPortType(); portType != wwanmodem.PortQMI && portType != wwanmodem.PortMBIM {
		return m.EquipmentIdentifier
	}
	return fmt.Sprintf("%s:%d", m.EquipmentIdentifier, slot)
}

// NewWithChannelFactory creates an LPA client using a fresh channel for each
// AID attempt. The returned client owns a successful channel; failed attempts
// are released before the next AID is tried.
func NewWithChannelFactory(ctx context.Context, cfg ChannelConfig, factory ChannelFactory) (*Client, error) {
	if factory == nil {
		return nil, errors.New("LPA channel factory is required")
	}
	if cfg.LockKey != "" {
		if err := operationLocks.LockContext(ctx, cfg.LockKey); err != nil {
			return nil, fmt.Errorf("reserve LPA client: %w", err)
		}
	}
	instance, err := newWithChannelFactoryLocked(ctx, cfg, factory)
	if err != nil {
		if cfg.LockKey != "" {
			operationLocks.Unlock(cfg.LockKey)
		}
		return nil, err
	}
	return instance, nil
}

func NewChannel(ctx context.Context, m *modem.Modem) (driver.SmartCardChannel, func(), error) {
	if m == nil {
		return nil, nil, errors.New("modem is required")
	}
	releaseSIMSlot, err := m.ReserveSIMSlot(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("reserve LPA SIM slot: %w", err)
	}
	slot, err := modem.ActiveSIMSlot(m)
	if err != nil {
		releaseSIMSlot()
		return nil, nil, err
	}
	key := lpaLockKey(m, slot)
	if err := operationLocks.LockContext(ctx, key); err != nil {
		releaseSIMSlot()
		return nil, nil, err
	}
	ch, err := createChannelForSlot(ctx, m, slot)
	if err != nil {
		operationLocks.Unlock(key)
		releaseSIMSlot()
		return nil, nil, err
	}
	locked := &lockedChannel{SmartCardChannel: ch, key: key, releaseSIMSlot: releaseSIMSlot}
	logger := m.Logger()
	release := func() {
		if err := locked.Disconnect(); err != nil {
			logger.Debug("disconnect LPA channel", "error", err)
		}
	}
	return locked, release, nil
}

type lockedChannel struct {
	driver.SmartCardChannel
	key            string
	releaseSIMSlot func()
	once           sync.Once
}

func (c *lockedChannel) Disconnect() error {
	var err error
	c.once.Do(func() {
		err = c.SmartCardChannel.Disconnect()
		operationLocks.Unlock(c.key)
		if c.releaseSIMSlot != nil {
			c.releaseSIMSlot()
		}
	})
	return err
}

func (c *lockedChannel) CloseLogicalChannel(channel byte) error {
	if err := c.SmartCardChannel.CloseLogicalChannel(channel); err != nil {
		return errors.Join(err, c.Disconnect())
	}
	return nil
}

func newWithChannelFactoryLocked(ctx context.Context, cfg ChannelConfig, factory ChannelFactory) (*Client, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	currentSettings := cfg.Settings
	if currentSettings == nil {
		currentSettings = settings.Default()
	}
	operation := newOperationContext(ctx)
	instance := &Client{
		clientView: &clientView{},
		lockKey:    cfg.LockKey,
		logger:     logger,
		operation:  operation,
	}
	opts := &lpa.Options{
		AdminProtocolVersion: "2.2.0",
		Logger:               logger,
		MSS:                  currentSettings.FindModem(cfg.ConfigID).MSS,
	}
	if len(cfg.AID) > 0 {
		if err := instance.tryCreateClient(ctx, operation, opts, factory, [][]byte{cfg.AID}); err != nil {
			return nil, err
		}
		return instance, nil
	}
	if err := instance.tryCreateClient(ctx, operation, opts, factory, supportedAIDs); err != nil {
		return nil, err
	}
	return instance, nil
}

func (l *Client) tryCreateClient(ctx context.Context, operation *operationContext, opts *lpa.Options, factory ChannelFactory, aids [][]byte) error {
	var errs error
	allUnsupported := true
	for _, aid := range aids {
		if err := ctx.Err(); err != nil {
			return errors.Join(errs, err)
		}
		opts.AID = slices.Clone(aid)
		channel, err := factory(ctx)
		if err != nil {
			return errors.Join(errs, fmt.Errorf("open channel for AID %X: %w", opts.AID, err))
		}
		if channel == nil {
			return errors.Join(errs, fmt.Errorf("open channel for AID %X: channel is nil", opts.AID))
		}
		ownedChannel := &disconnectOnceChannel{SmartCardChannel: channel}
		opts.Channel = &contextSmartCardChannel{operation: operation, SmartCardChannel: ownedChannel}
		client, err := newEUICCClient(operation, opts)
		if err == nil {
			l.clientView.client = client
			l.channel = ownedChannel
			l.logger.Info("LPA client created", "AID", fmt.Sprintf("%X", opts.AID))
			return nil
		}
		// euicc-go disconnects channels after transport setup failures, but it
		// validates options before taking ownership. The wrapper makes this
		// fallback safe in both cases.
		err = errors.Join(err, ownedChannel.Disconnect())
		l.logger.Warn("failed to create LPA client", "AID", fmt.Sprintf("%X", opts.AID), "error", err)
		allUnsupported = allUnsupported && errors.Is(err, errAIDNotSupported)
		errs = errors.Join(errs, fmt.Errorf("open AID %X: %w", opts.AID, err))
	}
	result := errors.Join(ErrNoSupportedAID, errs)
	if allUnsupported && errs != nil {
		result = errors.Join(result, errCacheableNoSupportedAID)
	}
	return result
}

type disconnectOnceChannel struct {
	driver.SmartCardChannel
	once sync.Once
	err  error
}

func (c *disconnectOnceChannel) Disconnect() error {
	c.once.Do(func() {
		c.err = c.SmartCardChannel.Disconnect()
	})
	return c.err
}

func newEUICCClient(operation *operationContext, opts *lpa.Options) (*lpa.Client, error) {
	client, err := lpa.New(opts)
	if err != nil {
		return nil, err
	}
	transport := client.HTTP.Client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.HTTP.Client.Transport = &contextRoundTripper{operation: operation, next: transport}
	return client, nil
}

// operationContext bridges request cancellation into euicc-go APIs that do not
// accept a context. Pool leases serialize updates through the same entry gate.
type operationContext struct {
	mu      sync.RWMutex
	base    context.Context
	current context.Context
}

func newOperationContext(ctx context.Context) *operationContext {
	return &operationContext{base: ctx}
}

func (c *operationContext) context() context.Context {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.current != nil {
		return c.current
	}
	return c.base
}

func (c *operationContext) use(ctx context.Context) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.current = ctx
	c.mu.Unlock()
}

func (c *operationContext) reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.current = nil
	c.mu.Unlock()
}

type contextRoundTripper struct {
	operation *operationContext
	next      http.RoundTripper
}

func (t *contextRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := t.operation.context()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return t.next.RoundTrip(req.Clone(ctx))
}

type contextSmartCardChannel struct {
	operation *operationContext
	driver.SmartCardChannel
}

type smartCardChannelWithContext interface {
	OpenLogicalChannelContext(context.Context, []byte) (byte, error)
	TransmitContext(context.Context, []byte) ([]byte, error)
}

type smartCardChannelCloserWithContext interface {
	CloseLogicalChannelContext(context.Context, byte) error
}

func (c *contextSmartCardChannel) Connect() error {
	if err := c.operation.context().Err(); err != nil {
		return err
	}
	return c.SmartCardChannel.Connect()
}

func (c *contextSmartCardChannel) OpenLogicalChannel(aid []byte) (byte, error) {
	ctx := c.operation.context()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if channel, ok := c.SmartCardChannel.(smartCardChannelWithContext); ok {
		return channel.OpenLogicalChannelContext(ctx, aid)
	}
	return c.SmartCardChannel.OpenLogicalChannel(aid)
}

func (c *contextSmartCardChannel) Transmit(command []byte) ([]byte, error) {
	ctx := c.operation.context()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if channel, ok := c.SmartCardChannel.(smartCardChannelWithContext); ok {
		return channel.TransmitContext(ctx, command)
	}
	return c.SmartCardChannel.Transmit(command)
}

func (c *contextSmartCardChannel) CloseLogicalChannel(channel byte) error {
	ctx := context.WithoutCancel(c.operation.context())
	if closer, ok := c.SmartCardChannel.(smartCardChannelCloserWithContext); ok {
		return closer.CloseLogicalChannelContext(ctx, channel)
	}
	return c.SmartCardChannel.CloseLogicalChannel(channel)
}

func createChannel(ctx context.Context, m *modem.Modem) (driver.SmartCardChannel, error) {
	slot, err := modem.ActiveSIMSlot(m)
	if err != nil {
		return nil, err
	}
	return createChannelForSlot(ctx, m, slot)
}

func createChannelForSlot(ctx context.Context, m *modem.Modem, slot uint8) (driver.SmartCardChannel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	portType := m.PrimaryPortType()
	switch portType {
	case wwanmodem.PortQMI:
		return createQMIChannel(ctx, m, slot)
	case wwanmodem.PortMBIM:
		return createMBIMChannel(ctx, m, slot)
	default:
		return nil, fmt.Errorf("create LPA channel for primary port type %d: %w", portType, wwanmodem.ErrNotSupported)
	}
}

func createQMIChannel(ctx context.Context, m *modem.Modem, slot uint8) (driver.SmartCardChannel, error) {
	ctx, cancel := context.WithTimeout(ctx, channelOpenTimeout)
	defer cancel()
	m.Logger().Info("using QMI driver", "port", m.PrimaryPort, "slot", slot)
	core := m.WWAN()
	if core == nil {
		return nil, errors.New("create QMI LPA channel: wwan modem is unavailable")
	}
	client, err := core.QMIClient(ctx, qmiPrimaryLogicalSlot)
	if err != nil {
		return nil, fmt.Errorf("open QMI LPA client: %w", err)
	}
	m.Logger().Debug("opened QMI LPA slot", "physicalSlot", slot, "logicalSlot", qmiPrimaryLogicalSlot)
	channel, err := newQMILPAChannel(client)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create QMI LPA channel: %w", err), client.Close())
	}
	return channel, nil
}

func createMBIMChannel(ctx context.Context, m *modem.Modem, slot uint8) (driver.SmartCardChannel, error) {
	ctx, cancel := context.WithTimeout(ctx, channelOpenTimeout)
	defer cancel()
	m.Logger().Info("using MBIM driver", "port", m.PrimaryPort, "slot", slot)
	core := m.WWAN()
	if core == nil {
		return nil, errors.New("create MBIM LPA channel: wwan modem is unavailable")
	}
	client, err := core.MBIMClient(ctx, slot)
	if err != nil {
		return nil, fmt.Errorf("open MBIM LPA client: %w", err)
	}
	channel, err := newMBIMLPAChannel(client)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create MBIM LPA channel: %w", err), client.Close())
	}
	return channel, nil
}

func (l *Client) Close() error {
	return l.closeWith(func() error {
		client := l.rawClient()
		if client == nil {
			return nil
		}
		return client.Close()
	})
}

// discard releases transport resources after a SIM change has already
// invalidated the old logical channel.
func (l *Client) discard() error {
	return l.closeWith(func() error {
		if l.channel != nil {
			return l.channel.Disconnect()
		}
		if client := l.rawClient(); client != nil {
			return client.Close()
		}
		return nil
	})
}

func (l *Client) rawClient() *lpa.Client {
	if l == nil || l.clientView == nil {
		return nil
	}
	return l.clientView.client
}

func (l *Client) closeWith(closeResource func() error) error {
	if l == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		if l.shutdown != nil {
			l.closeErr = l.shutdown()
		} else {
			l.closeErr = closeResource()
		}
		if l.lockKey != "" {
			operationLocks.Unlock(l.lockKey)
		}
		if l.releaseSIMSlot != nil {
			l.releaseSIMSlot()
		}
	})
	return l.closeErr
}

func (l *Client) Logger() *slog.Logger {
	return l.logger
}

func (l *Client) Info() (*Info, error) {
	var info Info
	eid, err := l.EID()
	if err != nil {
		return nil, fmt.Errorf("read EID: %w", err)
	}
	info.EID = hex.EncodeToString(eid)

	tlv, err := l.EUICCInfo2()
	if err != nil {
		return nil, fmt.Errorf("read eUICC info: %w", err)
	}
	if err := parseEUICCInfo2(&info, tlv); err != nil {
		return nil, err
	}
	return &info, nil
}

func parseEUICCInfo2(info *Info, tlv *bertlv.TLV) error {
	if info == nil {
		return errors.New("eUICC info destination is nil")
	}
	if tlv == nil {
		return errors.New("eUICC info is empty")
	}

	sasup := tlv.First(bertlv.Universal.Primitive(12))
	if sasup == nil {
		return errors.New("read SAS-UP version: field is missing")
	}
	info.SASUP = euicc.LookupSASUP(info.EID, string(sasup.Value))

	certificateList := tlv.First(bertlv.ContextSpecific.Constructed(10))
	if certificateList == nil {
		return errors.New("read certificate issuers: field is missing")
	}
	for _, child := range certificateList.Children {
		info.Certificates = append(info.Certificates, euicc.LookupCertificateIssuer(hex.EncodeToString(child.Value)))
	}

	resource := tlv.First(bertlv.ContextSpecific.Primitive(4))
	if resource == nil {
		return errors.New("read free non-volatile memory: resource field is missing")
	}
	data, err := resource.MarshalBinary()
	if err != nil {
		return fmt.Errorf("encode free non-volatile memory resource: %w", err)
	}
	if len(data) == 0 {
		return errors.New("decode free non-volatile memory resource: field is empty")
	}
	data[0] = 0x30
	if err := resource.UnmarshalBinary(data); err != nil {
		return fmt.Errorf("decode free non-volatile memory resource: %w", err)
	}
	freeSpace := resource.First(bertlv.ContextSpecific.Primitive(2))
	if freeSpace == nil {
		return errors.New("read free non-volatile memory: field is missing")
	}
	if err := primitive.UnmarshalInt(&info.FreeSpace).UnmarshalBinary(freeSpace.Value); err != nil {
		return fmt.Errorf("decode free non-volatile memory: %w", err)
	}
	return nil
}

func (l *Client) Delete(id sgp22.ICCID) error {
	currentNotifications, err := l.ListNotification()
	if err != nil {
		return err
	}
	var lastSeq sgp22.SequenceNumber
	for _, n := range currentNotifications {
		lastSeq = max(n.SequenceNumber, lastSeq)
	}

	if err := l.DeleteProfile(id); err != nil {
		return err
	}

	deletionNotifications, err := l.ListNotification(sgp22.NotificationEventDelete)
	if err != nil {
		return err
	}
	var errs error
	for _, n := range deletionNotifications {
		if n.SequenceNumber > lastSeq && bytes.Equal(n.ICCID, id) {
			l.logger.Info("sending deletion notification", "sequence", n.SequenceNumber)
			if err := l.SendNotification(n.SequenceNumber, false); err != nil {
				errs = errors.Join(errs, err)
			}
		}
	}
	return errs
}

func (l *Client) SendNotification(searchCriteria any, delete bool) error {
	notifications, err := l.RetrieveNotificationList(searchCriteria)
	if err != nil {
		return err
	}
	var errs error
	for _, notification := range notifications {
		if err := l.HandleNotification(notification); err != nil {
			errs = errors.Join(errs, err)
		}
		if delete {
			if err := l.RemoveNotificationFromList(notification.Notification.SequenceNumber); err != nil {
				errs = errors.Join(errs, err)
			}
		}
	}
	return errs
}

func (l *Client) Download(ctx context.Context, activationCode *lpa.ActivationCode, opts *lpa.DownloadOptions) error {
	l.logger.Info("downloading profile", "activationCode", activationCode)
	result, err := l.DownloadProfile(ctx, activationCode, opts)
	if err != nil {
		return err
	}
	if result != nil && result.Notification != nil && result.Notification.SequenceNumber > 0 {
		l.logger.Info("sending download notification", "sequence", result.Notification.SequenceNumber)
		if err := l.SendNotification(result.Notification.SequenceNumber, false); err != nil {
			return err
		}
	}
	return nil
}

func (l *Client) Discovery(imei sgp22.IMEI) ([]*sgp22.EventEntry, error) {
	var entries []*sgp22.EventEntry
	var errs error
	addresses := []url.URL{
		{Scheme: "https", Host: "lpa.ds.gsma.com"},
		{Scheme: "https", Host: "lpa.live.esimdiscovery.com"},
	}
	for _, address := range addresses {
		l.logger.Info("discovering profiles", "address", address.Host)
		discovered, err := l.rawClient().Discovery(&address, imei)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("discover profiles from %s: %w", address.Host, err))
			continue
		}
		for _, entry := range discovered {
			if entry == nil {
				continue
			}
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 && errs != nil {
		return nil, errs
	}
	return entries, nil
}
