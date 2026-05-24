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
	tat "github.com/damonto/ts43-go/driver/at"
	tccid "github.com/damonto/ts43-go/driver/ccid"
	tmbim "github.com/damonto/ts43-go/driver/mbim"
	tqmi "github.com/damonto/ts43-go/driver/qmi"
	"github.com/damonto/ts43-go/ts43"

	"github.com/damonto/sigmo/internal/pkg/config"
	ilpa "github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func (s *Service) openSource(ctx context.Context, cfg *config.Config, start Start) (*sourceEndpoint, error) {
	switch start.SourceType {
	case SourceModem:
		modem, err := s.registry.Find(ctx, start.SourceID)
		if err != nil {
			return nil, err
		}
		channel, release, err := openModemSource(modem)
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
		reader, err := tccid.NewWithReader(start.SourceID)
		if err != nil {
			return nil, fmt.Errorf("open CCID reader: %w", err)
		}
		return &sourceEndpoint{
			channel: reader,
			release: func() {
				if err := reader.Disconnect(); err != nil {
					slog.Debug("disconnect CCID reader", "error", err)
				}
			},
			device: ts43Device(start.SourceIMEI),
		}, nil
	default:
		return nil, ErrSourceUnsupported
	}
}

func openModemSource(modem *mmodem.Modem) (ts43.Channel, func(), error) {
	slot := uint8(1)
	if modem.PrimarySimSlot > 0 {
		slot = uint8(modem.PrimarySimSlot)
	}
	switch modem.PrimaryPortType() {
	case mmodem.ModemPortTypeQmi:
		ch, err := tqmi.New(modem.PrimaryPort, slot)
		if err != nil {
			return nil, nil, err
		}
		return ch, releaseSource(ch), nil
	case mmodem.ModemPortTypeMbim:
		ch, err := tmbim.New(modem.PrimaryPort, slot)
		if err != nil {
			return nil, nil, err
		}
		return ch, releaseSource(ch), nil
	default:
		port, err := modem.Port(mmodem.ModemPortTypeAt)
		if err != nil {
			return nil, nil, err
		}
		ch, err := tat.New(port.Device)
		if err != nil {
			return nil, nil, err
		}
		return ch, releaseSource(ch), nil
	}
}

type sourceCloser interface {
	Disconnect() error
}

func releaseSource(ch sourceCloser) func() {
	return func() {
		if err := ch.Disconnect(); err != nil {
			slog.Debug("disconnect transfer source", "error", err)
		}
	}
}

func (s *Service) activateSourceProfile(ctx context.Context, cfg *config.Config, start Start, candidate profileCandidate) error {
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
		return enableCCIDSourceProfile(cfg, start, iccid)
	default:
		return ErrSourceUnsupported
	}
}

func enableCCIDSourceProfile(cfg *config.Config, start Start, iccid sgp22.ICCID) error {
	reader, err := openCCIDLPAReader(start.SourceID)
	if err != nil {
		return fmt.Errorf("open CCID reader: %w", err)
	}
	client, err := ilpa.NewWithChannel(sourceLockKey(start.SourceType, start.SourceID), "", reader, cfg)
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

func (s *Service) deleteSourceProfile(ctx context.Context, cfg *config.Config, start Start, rawICCID string) error {
	iccid, err := sgp22.NewICCID(rawICCID)
	if err != nil {
		return fmt.Errorf("parse source ICCID: %w", err)
	}
	if strings.HasPrefix(start.ProfileID, string(ProfilePhysical)+":") {
		return errPhysicalSourceDeletion
	}
	switch start.SourceType {
	case SourceModem:
		return s.deleteModemSourceProfile(ctx, cfg, start, iccid)
	case SourceCCID:
		return deleteCCIDSourceProfile(cfg, start, iccid)
	default:
		return ErrSourceUnsupported
	}
}

func (s *Service) deleteModemSourceProfile(ctx context.Context, cfg *config.Config, start Start, iccid sgp22.ICCID) error {
	modem, err := s.registry.Find(ctx, start.SourceID)
	if err != nil {
		return err
	}
	profiles, err := sourceModemProfiles(modem, cfg)
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
	client, err := ilpa.New(modem, cfg)
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

func sourceModemProfiles(modem *mmodem.Modem, cfg *config.Config) ([]*sgp22.ProfileInfo, error) {
	client, err := ilpa.New(modem, cfg)
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

func deleteCCIDSourceProfile(cfg *config.Config, start Start, iccid sgp22.ICCID) error {
	reader, err := openCCIDLPAReader(start.SourceID)
	if err != nil {
		return fmt.Errorf("open CCID reader: %w", err)
	}
	client, err := ilpa.NewWithChannel(sourceLockKey(start.SourceType, start.SourceID), "", reader, cfg)
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
	reader, err := tccid.New()
	if err != nil {
		slog.Debug("list CCID readers", "error", err)
		if ccidServiceUnavailable(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() {
		if cerr := reader.Disconnect(); cerr != nil {
			slog.Debug("close CCID reader list context", "error", cerr)
		}
	}()
	readers, err := reader.ListReaders()
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

func modemName(cfg *config.Config, modem *mmodem.Modem) string {
	if alias := cfg.FindModem(modem.EquipmentIdentifier).Alias; alias != "" {
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
