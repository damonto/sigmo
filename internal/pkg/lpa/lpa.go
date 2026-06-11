package lpa

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"

	"github.com/damonto/euicc-go/apdu"
	"github.com/damonto/euicc-go/bertlv"
	"github.com/damonto/euicc-go/bertlv/primitive"
	"github.com/damonto/euicc-go/driver/at"
	"github.com/damonto/euicc-go/driver/mbim"
	"github.com/damonto/euicc-go/driver/qcom"
	"github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/euicc"
	"github.com/damonto/sigmo/internal/pkg/keymutex"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

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
	Channel  apdu.SmartCardChannel
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

func New(m *modem.Modem, currentSettings *settings.Settings) (*LPA, error) {
	gmu.Lock(m.EquipmentIdentifier)
	ch, err := createChannel(m)
	if err != nil {
		gmu.Unlock(m.EquipmentIdentifier)
		return nil, err
	}
	instance, err := newWithChannelLocked(ChannelConfig{
		LockKey:  m.EquipmentIdentifier,
		ConfigID: m.EquipmentIdentifier,
		Channel:  ch,
		Settings: currentSettings,
		Logger:   m.Logger(),
	})
	if err != nil {
		_ = ch.Disconnect()
		gmu.Unlock(m.EquipmentIdentifier)
		return nil, err
	}
	return instance, nil
}

func NewWithChannel(cfg ChannelConfig) (*LPA, error) {
	if cfg.Channel == nil {
		return nil, errors.New("lpa channel is required")
	}
	if cfg.LockKey != "" {
		gmu.Lock(cfg.LockKey)
	}
	instance, err := newWithChannelLocked(cfg)
	if err != nil {
		if cfg.LockKey != "" {
			gmu.Unlock(cfg.LockKey)
		}
		_ = cfg.Channel.Disconnect()
		return nil, err
	}
	return instance, nil
}

func NewChannel(m *modem.Modem) (apdu.SmartCardChannel, func(), error) {
	gmu.Lock(m.EquipmentIdentifier)
	ch, err := createChannel(m)
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
	apdu.SmartCardChannel
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

func newWithChannelLocked(cfg ChannelConfig) (*LPA, error) {
	logger := cfg.Logger
	currentSettings := cfg.Settings
	if currentSettings == nil {
		currentSettings = settings.Default()
	}
	instance := &LPA{lockKey: cfg.LockKey, logger: logger}
	opts := &lpa.Options{
		Channel:              cfg.Channel,
		AdminProtocolVersion: "2.2.0",
		Logger:               logger,
		MSS:                  currentSettings.FindModem(cfg.ConfigID).MSS,
	}
	if err := instance.tryCreateClient(opts); err != nil {
		return nil, err
	}
	return instance, nil
}

func (l *LPA) tryCreateClient(opts *lpa.Options) error {
	var err error
	for _, opts.AID = range AIDs {
		l.Client, err = lpa.New(opts)
		if err == nil {
			l.logger.Info("LPA client created", "AID", fmt.Sprintf("%X", opts.AID))
			return nil
		}
		l.logger.Warn("failed to create LPA client", "AID", fmt.Sprintf("%X", opts.AID), "error", err)
	}
	return ErrNoSupportedAID
}

func createChannel(m *modem.Modem) (apdu.SmartCardChannel, error) {
	slot := uint8(1)
	if m.PrimarySimSlot > 0 {
		slot = uint8(m.PrimarySimSlot)
	}
	switch m.PrimaryPortType() {
	case modem.ModemPortTypeQmi:
		m.Logger().Info("using QMI driver", "port", m.PrimaryPort, "slot", slot)
		return qcom.NewQMI(m.PrimaryPort, slot)
	case modem.ModemPortTypeMbim:
		m.Logger().Info("using MBIM driver", "port", m.PrimaryPort, "slot", slot)
		return mbim.New(m.PrimaryPort, slot)
	default:
		return createATChannel(m)
	}
}

func createATChannel(m *modem.Modem) (apdu.SmartCardChannel, error) {
	port, err := m.Port(modem.ModemPortTypeAt)
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
