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
	"github.com/damonto/euicc-go/driver/at"
	"github.com/damonto/euicc-go/driver/mbim"
	"github.com/damonto/euicc-go/driver/qcom"
	"github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/euicc"
	"github.com/damonto/sigmo/internal/pkg/keymutex"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

const channelOpenTimeout = 30 * time.Second

// gmu serializes LPA operations for the same modem or external reader. This is necessary
// because eUICC operations cannot safely share one underlying smart-card channel.
var gmu = keymutex.New()

type LPA struct {
	*lpa.Client
	lockKey string
	logger  *slog.Logger
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
	Channel  driver.SmartCardChannel
	AID      []byte
	Settings *settings.Settings
	Logger   *slog.Logger
}

var ErrNoSupportedAID = errors.New("no supported ISD-R AID found or it's not an eUICC")

var AIDs = [][]byte{
	lpa.GSMAISDRApplicationAID,
	{0xA0, 0x00, 0x00, 0x05, 0x59, 0x10, 0x10, 0xFF, 0xFF, 0xFF, 0xFF, 0x89, 0x00, 0x05, 0x05, 0x00}, // 5ber Ultra
	{0xA0, 0x00, 0x00, 0x05, 0x59, 0x10, 0x10, 0x00, 0x00, 0x00, 0x89, 0x00, 0x00, 0x00, 0x03, 0x00}, // eSIM.me V2
	{0xA0, 0x65, 0x73, 0x74, 0x6B, 0x6D, 0x65, 0xFF, 0xFF, 0xFF, 0xFF, 0x49, 0x53, 0x44, 0x2D, 0x52}, // ESTKme 2025
	{0xA0, 0x00, 0x00, 0x05, 0x59, 0x10, 0x10, 0xFF, 0xFF, 0xFF, 0xFF, 0x89, 0x00, 0x00, 0x01, 0x77}, // XeSIM
	{0xA0, 0x00, 0x00, 0x06, 0x28, 0x10, 0x10, 0xFF, 0xFF, 0xFF, 0xFF, 0x89, 0x00, 0x00, 0x01, 0x00}, // GlocalMe
}

func New(ctx context.Context, m *modem.Modem, currentSettings *settings.Settings) (*LPA, error) {
	return newForAID(ctx, m, currentSettings, nil)
}

func NewWithAID(ctx context.Context, m *modem.Modem, currentSettings *settings.Settings, aid []byte) (*LPA, error) {
	return newForAID(ctx, m, currentSettings, aid)
}

func newForAID(ctx context.Context, m *modem.Modem, currentSettings *settings.Settings, aid []byte) (*LPA, error) {
	if err := gmu.LockContext(ctx, m.EquipmentIdentifier); err != nil {
		return nil, fmt.Errorf("reserve LPA client: %w", err)
	}
	ch, err := createChannel(ctx, m)
	if err != nil {
		gmu.Unlock(m.EquipmentIdentifier)
		return nil, err
	}
	instance, err := newWithChannelLocked(ctx, ChannelConfig{
		LockKey:  m.EquipmentIdentifier,
		ConfigID: m.EquipmentIdentifier,
		Channel:  ch,
		AID:      aid,
		Settings: currentSettings,
		Logger:   m.Logger(),
	})
	if err != nil {
		if disconnectErr := ch.Disconnect(); disconnectErr != nil {
			m.Logger().Debug("disconnect LPA channel after client creation failure", "error", disconnectErr)
		}
		gmu.Unlock(m.EquipmentIdentifier)
		return nil, err
	}
	return instance, nil
}

func NewWithChannel(ctx context.Context, cfg ChannelConfig) (*LPA, error) {
	if cfg.Channel == nil {
		return nil, errors.New("lpa channel is required")
	}
	if cfg.LockKey != "" {
		if err := gmu.LockContext(ctx, cfg.LockKey); err != nil {
			return nil, errors.Join(fmt.Errorf("reserve LPA client: %w", err), cfg.Channel.Disconnect())
		}
	}
	instance, err := newWithChannelLocked(ctx, cfg)
	if err != nil {
		if cfg.LockKey != "" {
			gmu.Unlock(cfg.LockKey)
		}
		if disconnectErr := cfg.Channel.Disconnect(); disconnectErr != nil && cfg.Logger != nil {
			cfg.Logger.Debug("disconnect LPA channel after client creation failure", "error", disconnectErr)
		}
		return nil, err
	}
	return instance, nil
}

func NewChannel(ctx context.Context, m *modem.Modem) (driver.SmartCardChannel, func(), error) {
	if err := gmu.LockContext(ctx, m.EquipmentIdentifier); err != nil {
		return nil, nil, err
	}
	ch, err := createChannel(ctx, m)
	if err != nil {
		gmu.Unlock(m.EquipmentIdentifier)
		return nil, nil, err
	}
	locked := &lockedChannel{SmartCardChannel: ch, key: m.EquipmentIdentifier}
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
	key  string
	once sync.Once
}

func (c *lockedChannel) Disconnect() error {
	var err error
	c.once.Do(func() {
		err = c.SmartCardChannel.Disconnect()
		gmu.Unlock(c.key)
	})
	return err
}

func (c *lockedChannel) CloseLogicalChannel(channel byte) error {
	if err := c.SmartCardChannel.CloseLogicalChannel(channel); err != nil {
		return errors.Join(err, c.Disconnect())
	}
	return nil
}

func newWithChannelLocked(ctx context.Context, cfg ChannelConfig) (*LPA, error) {
	logger := cfg.Logger
	currentSettings := cfg.Settings
	if currentSettings == nil {
		currentSettings = settings.Default()
	}
	channel := &contextSmartCardChannel{ctx: ctx, SmartCardChannel: cfg.Channel}
	instance := &LPA{lockKey: cfg.LockKey, logger: logger}
	opts := &lpa.Options{
		Channel:              channel,
		AdminProtocolVersion: "2.2.0",
		Logger:               logger,
		MSS:                  currentSettings.FindModem(cfg.ConfigID).MSS,
	}
	if len(cfg.AID) > 0 {
		opts.AID = slices.Clone(cfg.AID)
		client, err := newEUICCClient(ctx, opts)
		if err != nil {
			logger.Warn("failed to create LPA client", "AID", fmt.Sprintf("%X", opts.AID), "error", err)
			return nil, errors.Join(ErrNoSupportedAID, fmt.Errorf("open AID %X: %w", opts.AID, err))
		}
		instance.Client = client
		logger.Info("LPA client created", "AID", fmt.Sprintf("%X", opts.AID))
		return instance, nil
	}
	if err := instance.tryCreateClient(ctx, opts); err != nil {
		return nil, err
	}
	return instance, nil
}

func (l *LPA) tryCreateClient(ctx context.Context, opts *lpa.Options) error {
	var errs error
	for _, opts.AID = range AIDs {
		if err := ctx.Err(); err != nil {
			return errors.Join(errs, err)
		}
		var err error
		l.Client, err = newEUICCClient(ctx, opts)
		if err == nil {
			l.logger.Info("LPA client created", "AID", fmt.Sprintf("%X", opts.AID))
			return nil
		}
		l.logger.Warn("failed to create LPA client", "AID", fmt.Sprintf("%X", opts.AID), "error", err)
		errs = errors.Join(errs, fmt.Errorf("open AID %X: %w", opts.AID, err))
	}
	return errors.Join(ErrNoSupportedAID, errs)
}

func newEUICCClient(ctx context.Context, opts *lpa.Options) (*lpa.Client, error) {
	client, err := lpa.New(opts)
	if err != nil {
		return nil, err
	}
	transport := client.HTTP.Client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.HTTP.Client.Transport = &contextRoundTripper{ctx: ctx, next: transport}
	return client, nil
}

type contextRoundTripper struct {
	ctx  context.Context
	next http.RoundTripper
}

func (t *contextRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	return t.next.RoundTrip(req.Clone(t.ctx))
}

type contextSmartCardChannel struct {
	ctx context.Context
	driver.SmartCardChannel
}

func (c *contextSmartCardChannel) Connect() error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	return c.SmartCardChannel.Connect()
}

func (c *contextSmartCardChannel) OpenLogicalChannel(aid []byte) (byte, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.SmartCardChannel.OpenLogicalChannel(aid)
}

func (c *contextSmartCardChannel) Transmit(command []byte) ([]byte, error) {
	if err := c.ctx.Err(); err != nil {
		return nil, err
	}
	return c.SmartCardChannel.Transmit(command)
}

func createChannel(ctx context.Context, m *modem.Modem) (driver.SmartCardChannel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch m.PrimaryPortType() {
	case wwanmodem.PortQMI:
		return createQMIChannel(ctx, m)
	case wwanmodem.PortMBIM:
		return createMBIMChannel(ctx, m)
	default:
		return createATChannel(m)
	}
}

func createQMIChannel(ctx context.Context, m *modem.Modem) (driver.SmartCardChannel, error) {
	ctx, cancel := context.WithTimeout(ctx, channelOpenTimeout)
	defer cancel()
	release, err := m.ReserveSIMSlot(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve QMI LPA SIM slot: %w", err)
	}
	reserved := true
	defer func() {
		if reserved {
			release()
		}
	}()

	slot, err := modem.ActiveSIMSlot(m)
	if err != nil {
		return nil, err
	}
	m.Logger().Info("using QMI driver", "port", m.PrimaryPort, "slot", slot)
	core := m.WWAN()
	if core == nil {
		return nil, errors.New("create QMI LPA channel: wwan modem is unavailable")
	}
	client, err := core.QMIClient(ctx, slot)
	if err != nil {
		return nil, fmt.Errorf("open QMI LPA client: %w", err)
	}
	channel, err := qcom.NewQMIWithClient(client)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create QMI LPA channel: %w", err), client.Close())
	}

	reserved = false
	return &simSlotChannel{SmartCardChannel: channel, release: release}, nil
}

func createMBIMChannel(ctx context.Context, m *modem.Modem) (driver.SmartCardChannel, error) {
	ctx, cancel := context.WithTimeout(ctx, channelOpenTimeout)
	defer cancel()
	release, err := m.ReserveSIMSlot(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve MBIM LPA SIM slot: %w", err)
	}
	reserved := true
	defer func() {
		if reserved {
			release()
		}
	}()

	slot, err := modem.ActiveSIMSlot(m)
	if err != nil {
		return nil, err
	}
	m.Logger().Info("using MBIM driver", "port", m.PrimaryPort, "slot", slot)
	core := m.WWAN()
	if core == nil {
		return nil, errors.New("create MBIM LPA channel: wwan modem is unavailable")
	}
	client, err := core.MBIMClient(ctx, slot)
	if err != nil {
		return nil, fmt.Errorf("open MBIM LPA client: %w", err)
	}
	channel, err := mbim.NewWithClient(client)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create MBIM LPA channel: %w", err), client.Close())
	}

	reserved = false
	return &simSlotChannel{SmartCardChannel: channel, release: release}, nil
}

type simSlotChannel struct {
	driver.SmartCardChannel
	release func()
	once    sync.Once
	err     error
}

func (c *simSlotChannel) Disconnect() error {
	c.once.Do(func() {
		c.err = c.SmartCardChannel.Disconnect()
		c.release()
	})
	return c.err
}

func (c *simSlotChannel) CloseLogicalChannel(channel byte) error {
	if err := c.SmartCardChannel.CloseLogicalChannel(channel); err != nil {
		return errors.Join(err, c.Disconnect())
	}
	return nil
}

func createATChannel(m *modem.Modem) (driver.SmartCardChannel, error) {
	port, err := m.Port(wwanmodem.PortAT)
	if err != nil {
		return nil, err
	}
	m.Logger().Info("using AT driver", "port", port.Device)
	return at.New(port.Device)
}

func (l *LPA) Close() error {
	err := l.Client.Close()
	if l.lockKey != "" {
		gmu.Unlock(l.lockKey)
	}
	return err
}

func (l *LPA) Logger() *slog.Logger {
	return l.logger
}

func (l *LPA) Info() (*Info, error) {
	var info Info
	eid, err := l.EID()
	if err != nil {
		return nil, err
	}
	info.EID = hex.EncodeToString(eid)

	tlv, err := l.EUICCInfo2()
	if err != nil {
		return nil, err
	}

	// SASUP
	info.SASUP = euicc.LookupSASUP(info.EID, string(tlv.First(bertlv.Universal.Primitive(12)).Value))

	// euiccCiPKIdListForSigning
	for _, child := range tlv.First(bertlv.ContextSpecific.Constructed(10)).Children {
		info.Certificates = append(info.Certificates, euicc.LookupCertificateIssuer(hex.EncodeToString(child.Value)))
	}

	// extResource.freeNonVolatileMemory
	resource := tlv.First(bertlv.ContextSpecific.Primitive(4))
	data, _ := resource.MarshalBinary()
	data[0] = 0x30
	if err := resource.UnmarshalBinary(data); err != nil {
		return nil, err
	}
	primitive.UnmarshalInt(&info.FreeSpace).UnmarshalBinary(resource.First(bertlv.ContextSpecific.Primitive(2)).Value)
	return &info, nil
}

func (l *LPA) Delete(id sgp22.ICCID) error {
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

func (l *LPA) SendNotification(searchCriteria any, delete bool) error {
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

func (l *LPA) Download(ctx context.Context, activationCode *lpa.ActivationCode, opts *lpa.DownloadOptions) error {
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

func (l *LPA) Discovery(imei sgp22.IMEI) ([]*sgp22.EventEntry, error) {
	var entries []*sgp22.EventEntry
	var errs error
	addresses := []url.URL{
		{Scheme: "https", Host: "lpa.ds.gsma.com"},
		{Scheme: "https", Host: "lpa.live.esimdiscovery.com"},
	}
	for _, address := range addresses {
		l.logger.Info("discovering profiles", "address", address.Host)
		discovered, err := l.Client.Discovery(&address, imei)
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
