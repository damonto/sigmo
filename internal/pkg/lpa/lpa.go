package lpa

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/damonto/euicc-go/apdu"
	"github.com/damonto/euicc-go/bertlv"
	"github.com/damonto/euicc-go/bertlv/primitive"
	"github.com/damonto/euicc-go/driver/at"
	"github.com/damonto/euicc-go/driver/mbim"
	"github.com/damonto/euicc-go/driver/qmi"
	"github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/euicc"
	"github.com/damonto/sigmo/internal/pkg/keymutex"
	"github.com/damonto/sigmo/internal/pkg/modem"
)

// gmu ensures that LPA instances for the same modem (keyed by EquipmentIdentifier)
// cannot be created or operated on concurrently. This is necessary because eUICC does not
// allow simultaneous operations on the same eUICC module.
var gmu = keymutex.New()

type LPA struct {
	*lpa.Client
	key string // EquipmentIdentifier used for global locking
}

type Info struct {
	EID          string
	FreeSpace    int32
	SASUP        string
	Certificates []string
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

func New(m *modem.Modem, cfg *config.Config) (*LPA, error) {
	gmu.Lock(m.EquipmentIdentifier)
	instance := &LPA{key: m.EquipmentIdentifier}
	ch, err := instance.createChannel(m)
	if err != nil {
		gmu.Unlock(m.EquipmentIdentifier)
		return nil, err
	}
	opts := &lpa.Options{
		Channel:              ch,
		AdminProtocolVersion: "2.2.0",
		MSS:                  cfg.FindModem(m.EquipmentIdentifier).MSS,
	}
	if err := instance.tryCreateClient(opts); err != nil {
		gmu.Unlock(m.EquipmentIdentifier)
		return nil, err
	}
	return instance, nil
}

func (l *LPA) tryCreateClient(opts *lpa.Options) error {
	var err error
	for _, opts.AID = range AIDs {
		l.Client, err = lpa.New(opts)
		if err == nil {
			slog.Info("LPA client created", "AID", fmt.Sprintf("%X", opts.AID))
			return nil
		}
		slog.Warn("failed to create LPA client", "AID", fmt.Sprintf("%X", opts.AID), "error", err)
	}
	return ErrNoSupportedAID
}

func (l *LPA) createChannel(m *modem.Modem) (apdu.SmartCardChannel, error) {
	slot := uint8(1)
	if m.PrimarySimSlot > 0 {
		slot = uint8(m.PrimarySimSlot)
	}
	switch m.PrimaryPortType() {
	case modem.ModemPortTypeQmi:
		slog.Info("using QMI driver", "port", m.PrimaryPort, "slot", slot)
		return qmi.New(m.PrimaryPort, slot)
	case modem.ModemPortTypeMbim:
		slog.Info("using MBIM driver", "port", m.PrimaryPort, "slot", slot)
		return mbim.New(m.PrimaryPort, slot)
	default:
		return l.createATChannel(m)
	}
}

func (l *LPA) createATChannel(m *modem.Modem) (apdu.SmartCardChannel, error) {
	port, err := m.Port(modem.ModemPortTypeAt)
	if err != nil {
		return nil, err
	}
	slog.Info("using AT driver", "port", port.Device)
	return at.New(port.Device)
}

func (l *LPA) Close() error {
	gmu.Unlock(l.key)
	return l.Client.Close()
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
			slog.Info("sending deletion notification", "sequence", n.SequenceNumber)
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
	slog.Info("downloading profile", "activationCode", activationCode)
	result, err := l.DownloadProfile(ctx, activationCode, opts)
	if err != nil {
		return err
	}
	if result != nil && result.Notification != nil && result.Notification.SequenceNumber > 0 {
		slog.Info("sending download notification", "sequence", result.Notification.SequenceNumber)
		if err := l.SendNotification(result.Notification.SequenceNumber, false); err != nil {
			return err
		}
	}
	return nil
}

func (l *LPA) Discover(imei sgp22.IMEI) ([]*sgp22.EventEntry, error) {
	var entries []*sgp22.EventEntry
	addresses := []url.URL{
		{Scheme: "https", Host: "lpa.ds.gsma.com"},
		{Scheme: "https", Host: "lpa.live.esimdiscovery.com"},
	}
	for _, address := range addresses {
		slog.Info("discovering profiles", "address", address.Host)
		discovered, err := l.Discovery(&address, imei)
		if err != nil {
			return nil, err
		}
		for _, entry := range discovered {
			if entry == nil {
				continue
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}
