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
	"github.com/damonto/wwan-go/at"
	"github.com/damonto/wwan-go/ccid"
	usim "github.com/damonto/wwan-go/sim"
	usimcard "github.com/damonto/wwan-go/sim/card"

	ilpa "github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

func (s *transferRunner) openSource(ctx context.Context, currentSettings *settings.Settings, start startRequest) (*sourceConnection, error) {
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
		return &sourceConnection{
			channel: channel,
			release: release,
			device:  device,
		}, nil
	case SourceCCID:
		channel, release, err := openCCIDSource(ctx, start.SourceID, sourceLogger(start))
		if err != nil {
			return nil, fmt.Errorf("open CCID reader: %w", err)
		}
		return &sourceConnection{
			channel: channel,
			release: release,
			device:  ts43Device(start.SourceIMEI),
		}, nil
	default:
		return nil, ErrSourceUnsupported
	}
}

func openModemSource(ctx context.Context, modem *mmodem.Modem) (ts43.Channel, func(), error) {
	logger := modem.Logger()
	device, err := mmodem.OpenDevice(modem)
	if err == nil {
		reader, err := device.USIM(ctx)
		if err != nil {
			return nil, nil, err
		}
		return sourceFromReader(ctx, reader, logger)
	}
	if !errors.Is(err, wwan.ErrUnsupported) {
		return nil, nil, err
	}
	reader, err := openATSource(ctx, modem)
	if err != nil {
		return nil, nil, err
	}
	return sourceFromReader(ctx, reader, logger)
}

type sourceCloser interface {
	Close() error
}

func releaseSource(ch sourceCloser, logger *slog.Logger) func() {
	return func() {
		if err := ch.Close(); err != nil {
			logger.Debug("disconnect transfer source", "error", err)
		}
	}
}

func openCCIDSource(ctx context.Context, readerName string, logger *slog.Logger) (ts43.Channel, func(), error) {
	tx, err := ccid.Open(ctx, readerName)
	if err != nil {
		return nil, nil, err
	}
	reader, err := usim.NewReader(tx)
	if err != nil {
		return nil, nil, errors.Join(err, tx.Close())
	}
	return sourceFromReader(ctx, reader, logger)
}

func openATSource(ctx context.Context, modem *mmodem.Modem) (usimcard.Reader, error) {
	port, err := modem.Port(mmodem.ModemPortTypeAt)
	if err != nil {
		return nil, err
	}
	tx, err := at.Open(port.Device, 0)
	if err != nil {
		return nil, err
	}
	reader, err := usim.NewReader(tx)
	if err != nil {
		return nil, errors.Join(err, tx.Close())
	}
	return reader, nil
}

func sourceFromReader(ctx context.Context, reader usimcard.Reader, logger *slog.Logger) (ts43.Channel, func(), error) {
	card, err := usim.New(ctx, reader, logger)
	if err != nil {
		return nil, nil, errors.Join(err, reader.Close())
	}
	source, err := ts43.NewSource(card)
	if err != nil {
		return nil, nil, errors.Join(err, card.Close())
	}
	return source, releaseSource(source, logger), nil
}

func (s *transferRunner) activateSourceProfile(ctx context.Context, currentSettings *settings.Settings, start startRequest, option ProfileResponse) error {
	if option.Type == ProfilePhysical {
		return nil
	}
	iccid, err := sgp22.NewICCID(option.ICCID)
	if err != nil {
		return fmt.Errorf("parse source ICCID: %w", err)
	}
	switch start.SourceType {
	case SourceModem:
		modem, err := s.registry.Find(ctx, start.SourceID)
		if err != nil {
			return err
		}
		return s.enableModemSourceProfile(ctx, modem, option.SEID, iccid)
	case SourceCCID:
		return enableCCIDSourceProfile(currentSettings, start, iccid)
	default:
		return ErrSourceUnsupported
	}
}

func enableCCIDSourceProfile(currentSettings *settings.Settings, start startRequest, iccid sgp22.ICCID) error {
	reader, err := openCCIDLPAReader(start.SourceID)
	if err != nil {
		return fmt.Errorf("open CCID reader: %w", err)
	}
	logger := sourceLogger(start)
	sourceLPA, err := ilpa.NewWithChannel(ilpa.ChannelConfig{
		LockKey:  sourceLockKey(start.SourceType, start.SourceID),
		Channel:  reader,
		Settings: currentSettings,
		Logger:   logger,
	})
	if err != nil {
		return fmt.Errorf("create source LPA client: %w", err)
	}
	defer func() {
		if cerr := sourceLPA.Close(); cerr != nil {
			sourceLPA.Logger().Warn("close source LPA client", "error", cerr)
		}
	}()
	profiles, err := sourceLPA.ListProfile(iccid, nil)
	if err != nil {
		return fmt.Errorf("list source profiles: %w", err)
	}
	if activeProfile(profiles, iccid) {
		return nil
	}
	if err := sourceLPA.EnableProfile(iccid, true); err != nil {
		return fmt.Errorf("enable source profile: %w", err)
	}
	return nil
}

func (s *transferRunner) deleteSourceProfile(ctx context.Context, currentSettings *settings.Settings, start startRequest, rawICCID string) error {
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

func (s *transferRunner) deleteModemSourceProfile(ctx context.Context, currentSettings *settings.Settings, start startRequest, iccid sgp22.ICCID) error {
	modem, err := s.registry.Find(ctx, start.SourceID)
	if err != nil {
		return err
	}
	profiles, err := sourceModemProfiles(modem, currentSettings)
	if err != nil {
		return err
	}
	if fallback, ok := fallbackModemProfile(profiles, iccid); ok {
		if err := s.enableModemSourceProfile(ctx, modem, fallback.seID, fallback.profile.ICCID); err != nil {
			return err
		}
		modem, err = s.registry.Find(ctx, start.SourceID)
		if err != nil {
			return err
		}
		return s.deleteModemProfile(ctx, modem, sourceProfileSEID(start.ProfileID), iccid)
	}
	se, err := ilpa.ResolveSE(modem, sourceProfileSEID(start.ProfileID))
	if err != nil {
		return fmt.Errorf("resolve source eUICC SE: %w", err)
	}
	sourceLPA, err := ilpa.NewWithAID(modem, currentSettings, se.AID)
	if err != nil {
		return fmt.Errorf("create source LPA client: %w", err)
	}
	defer func() {
		if cerr := sourceLPA.Close(); cerr != nil {
			sourceLPA.Logger().Warn("close source LPA client", "error", cerr)
		}
	}()
	if err := sourceLPA.Delete(iccid); err != nil {
		return fmt.Errorf("delete source profile: %w", err)
	}
	return nil
}

type sourceModemProfile struct {
	seID    string
	profile *sgp22.ProfileInfo
}

func sourceModemProfiles(modem *mmodem.Modem, currentSettings *settings.Settings) ([]sourceModemProfile, error) {
	ses, err := ilpa.DiscoverSEs(modem)
	if err != nil {
		return nil, fmt.Errorf("discover source eUICC SEs: %w", err)
	}
	out := []sourceModemProfile{}
	for _, se := range ses {
		sourceLPA, err := ilpa.NewWithAID(modem, currentSettings, se.AID)
		if err != nil {
			return nil, fmt.Errorf("create source LPA client for %s: %w", se.ID, err)
		}
		profiles, err := sourceLPA.ListProfile(nil, nil)
		if err != nil {
			if cerr := sourceLPA.Close(); cerr != nil {
				sourceLPA.Logger().Warn("close source LPA client", "error", cerr)
			}
			return nil, fmt.Errorf("list source profiles for %s: %w", se.ID, err)
		}
		for _, profile := range profiles {
			out = append(out, sourceModemProfile{seID: se.ID, profile: profile})
		}
		if cerr := sourceLPA.Close(); cerr != nil {
			sourceLPA.Logger().Warn("close source LPA client", "error", cerr)
		}
	}
	return out, nil
}

func (s *transferRunner) enableModemSourceProfile(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID) error {
	if s.enableProfile == nil {
		return errors.New("enable profile dependency is missing")
	}
	return s.enableProfile(ctx, modem, seID, iccid)
}

func (s *transferRunner) deleteModemProfile(ctx context.Context, modem *mmodem.Modem, seID string, iccid sgp22.ICCID) error {
	if s.deleteProfile == nil {
		return errors.New("delete profile dependency is missing")
	}
	return s.deleteProfile(ctx, modem, seID, iccid)
}

func deleteCCIDSourceProfile(currentSettings *settings.Settings, start startRequest, iccid sgp22.ICCID) error {
	reader, err := openCCIDLPAReader(start.SourceID)
	if err != nil {
		return fmt.Errorf("open CCID reader: %w", err)
	}
	logger := sourceLogger(start)
	sourceLPA, err := ilpa.NewWithChannel(ilpa.ChannelConfig{
		LockKey:  sourceLockKey(start.SourceType, start.SourceID),
		Channel:  reader,
		Settings: currentSettings,
		Logger:   logger,
	})
	if err != nil {
		return fmt.Errorf("create source LPA client: %w", err)
	}
	defer func() {
		if cerr := sourceLPA.Close(); cerr != nil {
			sourceLPA.Logger().Warn("close source LPA client", "error", cerr)
		}
	}()
	profiles, err := sourceLPA.ListProfile(nil, nil)
	if err != nil {
		return fmt.Errorf("list source profiles: %w", err)
	}
	if fallback, ok := fallbackProfile(profiles, iccid); ok && fallback.ProfileState != sgp22.ProfileEnabled {
		if err := sourceLPA.EnableProfile(fallback.ICCID, true); err != nil {
			return fmt.Errorf("enable source fallback profile: %w", err)
		}
	}
	if err := sourceLPA.Delete(iccid); err != nil {
		return fmt.Errorf("delete source profile: %w", err)
	}
	return nil
}

func sourceProfileSEID(profileID string) string {
	seID, _, ok := strings.Cut(profileID, ":")
	if !ok {
		return ilpa.SEIDDefault
	}
	return seID
}

func fallbackModemProfile(profiles []sourceModemProfile, source sgp22.ICCID) (sourceModemProfile, bool) {
	for _, profile := range profiles {
		if profile.profile == nil || profile.profile.ICCID.String() == source.String() {
			continue
		}
		return profile, true
	}
	return sourceModemProfile{}, false
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

func sourceLogger(start startRequest) *slog.Logger {
	switch start.SourceType {
	case SourceCCID:
		logger := mmodem.LoggerForIMEI(start.SourceIMEI)
		if reader := strings.TrimSpace(start.SourceID); reader != "" {
			logger = logger.With("reader", reader)
		}
		return logger
	case SourceModem:
		return mmodem.LoggerForIMEI(start.SourceID)
	default:
		return mmodem.LoggerForIMEI("")
	}
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
