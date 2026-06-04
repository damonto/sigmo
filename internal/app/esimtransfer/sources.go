//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	eccid "github.com/damonto/euicc-go/driver/ccid"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/ts43-go"
	"github.com/damonto/uicc-go/at"
	"github.com/damonto/uicc-go/ccid"
	"github.com/damonto/uicc-go/qualcomm/qmi"
	"github.com/damonto/uicc-go/qualcomm/uim"
	"github.com/damonto/uicc-go/usim"
	usimcard "github.com/damonto/uicc-go/usim/card"

	ilpa "github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

func (s *Service) openSource(ctx context.Context, currentSettings *settings.Settings, start Start) (*sourceEndpoint, error) {
	switch start.SourceType {
	case SourceModem:
		modem, err := s.registry.Find(ctx, start.SourceID)
		if err != nil {
			return nil, err
		}
		channel, release, err := openModemSource(ctx, modem)
		if err != nil {
			return nil, err
		}
		imei, err := modem.ThreeGPP().IMEI(ctx)
		if err != nil {
			release()
			return nil, fmt.Errorf("read source IMEI: %w", err)
		}
		device := ts43Device(imei)
		return &sourceEndpoint{
			channel: channel,
			release: release,
			device:  device,
		}, nil
	case SourceCCID:
		channel, release, err := openCCIDSource(ctx, start.SourceID)
		if err != nil {
			return nil, fmt.Errorf("open CCID reader: %w", err)
		}
		return &sourceEndpoint{
			channel: channel,
			release: release,
			device:  ts43Device(start.SourceIMEI),
		}, nil
	default:
		return nil, ErrSourceUnsupported
	}
}

func openModemSource(ctx context.Context, modem *mmodem.Modem) (ts43.Channel, func(), error) {
	sourcePort, err := selectModemSourcePort(modem)
	if err != nil {
		return nil, nil, err
	}
	switch sourcePort.portType {
	case mmodem.ModemPortTypeQmi:
		reader, err := openQMISource(ctx, sourcePort.device, sourcePort.slot)
		if err != nil {
			return nil, nil, err
		}
		return sourceFromReader(ctx, reader)
	case mmodem.ModemPortTypeAt:
		reader, err := openATSource(ctx, sourcePort.device)
		if err != nil {
			return nil, nil, err
		}
		return sourceFromReader(ctx, reader)
	default:
		return nil, nil, errors.New("modem source port type is unsupported")
	}
}

type modemSourcePort struct {
	portType mmodem.ModemPortType
	device   string
	slot     int
}

func selectModemSourcePort(modem *mmodem.Modem) (modemSourcePort, error) {
	slot := 1
	if modem.PrimarySimSlot > 0 {
		slot = int(modem.PrimarySimSlot)
	}
	switch modem.PrimaryPortType() {
	case mmodem.ModemPortTypeQmi:
		return modemSourcePort{portType: mmodem.ModemPortTypeQmi, device: modem.PrimaryPort, slot: slot}, nil
	default:
		port, err := modem.Port(mmodem.ModemPortTypeAt)
		if err != nil {
			return modemSourcePort{}, err
		}
		return modemSourcePort{portType: mmodem.ModemPortTypeAt, device: port.Device, slot: slot}, nil
	}
}

type sourceCloser interface {
	Close() error
}

func releaseSource(ch sourceCloser) func() {
	return func() {
		if err := ch.Close(); err != nil {
			slog.Debug("disconnect transfer source", "error", err)
		}
	}
}

func openCCIDSource(ctx context.Context, readerName string) (ts43.Channel, func(), error) {
	tx, err := ccid.Open(ctx, readerName)
	if err != nil {
		return nil, nil, err
	}
	reader, err := usim.NewReader(tx)
	if err != nil {
		return nil, nil, errors.Join(err, tx.Close())
	}
	return sourceFromReader(ctx, reader)
}

func openATSource(ctx context.Context, device string) (usimcard.Reader, error) {
	tx, err := at.Open(ctx, device, 0)
	if err != nil {
		return nil, err
	}
	reader, err := usim.NewReader(tx)
	if err != nil {
		return nil, errors.Join(err, tx.Close())
	}
	return reader, nil
}

func openQMISource(ctx context.Context, device string, slot int) (usimcard.Reader, error) {
	if slot < 1 || slot > 5 {
		return nil, fmt.Errorf("slot %d is out of range", slot)
	}
	transport, err := qmi.Open(ctx, qmi.WithProxy(device))
	if err != nil {
		return nil, err
	}
	reader, err := uim.New(ctx, transport, uim.WithSlot(uint8(slot)))
	if err != nil {
		return nil, errors.Join(err, transport.Close())
	}
	if err := reader.ActivateSlot(ctx); err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	adapter, err := usim.NewQualcomm(reader)
	if err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	return adapter, nil
}

func sourceFromReader(ctx context.Context, reader usimcard.Reader) (ts43.Channel, func(), error) {
	card, err := usim.New(ctx, reader, slog.Default())
	if err != nil {
		return nil, nil, errors.Join(err, reader.Close())
	}
	source, err := ts43.NewSource(card)
	if err != nil {
		return nil, nil, errors.Join(err, card.Close())
	}
	return source, releaseSource(source), nil
}

func (s *Service) activateSourceProfile(ctx context.Context, currentSettings *settings.Settings, start Start, candidate profileCandidate) error {
	if candidate.response.Type == ProfilePhysical {
		return nil
	}
	iccid, err := sgp22.NewICCID(candidate.response.ICCID)
	if err != nil {
		return fmt.Errorf("parse source ICCID: %w", err)
	}
	switch start.SourceType {
	case SourceModem:
		modem, err := s.registry.Find(ctx, start.SourceID)
		if err != nil {
			return err
		}
		return s.enableModemSourceProfile(ctx, modem, iccid)
	case SourceCCID:
		return enableCCIDSourceProfile(currentSettings, start, iccid)
	default:
		return ErrSourceUnsupported
	}
}

func enableCCIDSourceProfile(currentSettings *settings.Settings, start Start, iccid sgp22.ICCID) error {
	reader, err := openCCIDLPAReader(start.SourceID)
	if err != nil {
		return fmt.Errorf("open CCID reader: %w", err)
	}
	client, err := ilpa.NewWithChannel(sourceLockKey(start.SourceType, start.SourceID), "", reader, currentSettings)
	if err != nil {
		return fmt.Errorf("create source LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("close source LPA client", "error", cerr)
		}
	}()
	profiles, err := client.ListProfile(iccid, nil)
	if err != nil {
		return fmt.Errorf("list source profiles: %w", err)
	}
	if activeProfile(profiles, iccid) {
		return nil
	}
	if err := client.EnableProfile(iccid, true); err != nil {
		return fmt.Errorf("enable source profile: %w", err)
	}
	return nil
}

func (s *Service) deleteSourceProfile(ctx context.Context, currentSettings *settings.Settings, start Start, rawICCID string) error {
	iccid, err := sgp22.NewICCID(rawICCID)
	if err != nil {
		return fmt.Errorf("parse source ICCID: %w", err)
	}
	if strings.HasPrefix(start.ProfileID, string(ProfilePhysical)+":") {
		return errPhysicalSourceDeletion
	}
	switch start.SourceType {
	case SourceModem:
		return s.deleteModemSourceProfile(ctx, currentSettings, start, iccid)
	case SourceCCID:
		return deleteCCIDSourceProfile(currentSettings, start, iccid)
	default:
		return ErrSourceUnsupported
	}
}

func (s *Service) deleteModemSourceProfile(ctx context.Context, currentSettings *settings.Settings, start Start, iccid sgp22.ICCID) error {
	modem, err := s.registry.Find(ctx, start.SourceID)
	if err != nil {
		return err
	}
	profiles, err := sourceModemProfiles(modem, currentSettings)
	if err != nil {
		return err
	}
	if fallback, ok := fallbackProfile(profiles, iccid); ok {
		if err := s.enableModemSourceProfile(ctx, modem, fallback.ICCID); err != nil {
			return err
		}
		modem, err = s.registry.Find(ctx, start.SourceID)
		if err != nil {
			return err
		}
		return s.deleteModemProfile(ctx, modem, iccid)
	}
	client, err := ilpa.New(modem, currentSettings)
	if err != nil {
		return fmt.Errorf("create source LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("close source LPA client", "error", cerr)
		}
	}()
	if err := client.Delete(iccid); err != nil {
		return fmt.Errorf("delete source profile: %w", err)
	}
	return nil
}

func sourceModemProfiles(modem *mmodem.Modem, currentSettings *settings.Settings) ([]*sgp22.ProfileInfo, error) {
	client, err := ilpa.New(modem, currentSettings)
	if err != nil {
		return nil, fmt.Errorf("create source LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("close source LPA client", "error", cerr)
		}
	}()
	profiles, err := client.ListProfile(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list source profiles: %w", err)
	}
	return profiles, nil
}

func (s *Service) enableModemSourceProfile(ctx context.Context, modem *mmodem.Modem, iccid sgp22.ICCID) error {
	if s.enableProfile == nil {
		return errors.New("enable profile dependency is missing")
	}
	return s.enableProfile(ctx, modem, iccid)
}

func (s *Service) deleteModemProfile(ctx context.Context, modem *mmodem.Modem, iccid sgp22.ICCID) error {
	if s.deleteProfile == nil {
		return errors.New("delete profile dependency is missing")
	}
	return s.deleteProfile(ctx, modem, iccid)
}

func deleteCCIDSourceProfile(currentSettings *settings.Settings, start Start, iccid sgp22.ICCID) error {
	reader, err := openCCIDLPAReader(start.SourceID)
	if err != nil {
		return fmt.Errorf("open CCID reader: %w", err)
	}
	client, err := ilpa.NewWithChannel(sourceLockKey(start.SourceType, start.SourceID), "", reader, currentSettings)
	if err != nil {
		return fmt.Errorf("create source LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("close source LPA client", "error", cerr)
		}
	}()
	profiles, err := client.ListProfile(nil, nil)
	if err != nil {
		return fmt.Errorf("list source profiles: %w", err)
	}
	if fallback, ok := fallbackProfile(profiles, iccid); ok && fallback.ProfileState != sgp22.ProfileEnabled {
		if err := client.EnableProfile(fallback.ICCID, true); err != nil {
			return fmt.Errorf("enable source fallback profile: %w", err)
		}
	}
	if err := client.Delete(iccid); err != nil {
		return fmt.Errorf("delete source profile: %w", err)
	}
	return nil
}

func fallbackProfile(profiles []*sgp22.ProfileInfo, source sgp22.ICCID) (*sgp22.ProfileInfo, bool) {
	for _, profile := range profiles {
		if profile == nil || profile.ICCID.String() == source.String() {
			continue
		}
		return profile, true
	}
	return nil, false
}

func activeProfile(profiles []*sgp22.ProfileInfo, iccid sgp22.ICCID) bool {
	for _, profile := range profiles {
		if profile == nil || profile.ICCID.String() != iccid.String() {
			continue
		}
		return profile.ProfileState == sgp22.ProfileEnabled
	}
	return false
}

func listCCIDReaders() ([]string, error) {
	readers, err := ccid.ListReaders(context.Background())
	if err != nil {
		slog.Debug("list CCID readers", "error", err)
		if ccidServiceUnavailable(err) {
			return nil, nil
		}
		return nil, err
	}
	return readers, nil
}

func sourceLockKey(sourceType SourceType, sourceID string) string {
	return string(sourceType) + ":" + sourceID
}

func modemName(currentSettings *settings.Settings, modem *mmodem.Modem) string {
	if alias := currentSettings.FindModem(modem.EquipmentIdentifier).Alias; alias != "" {
		return alias
	}
	return modem.Model
}

func ccidServiceUnavailable(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, scardNoServiceName) || strings.Contains(msg, scardNoServiceCode)
}

func openCCIDLPAReader(readerName string) (eccid.CCID, error) {
	reader, err := eccid.New()
	if err != nil {
		return nil, err
	}
	reader.SetReader(readerName)
	return reader, nil
}
