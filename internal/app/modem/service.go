// Package modem coordinates modem inventory and SIM operations across transports.
package modem

import (
	"context"
	"errors"
	"time"

	"github.com/damonto/sigmo/internal/app/modemstatus"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// Config supplies modem observations and shared SIM operation dependencies.
type Config struct {
	Storage            *storage.Store
	Store              *settings.Store
	Registry           *mmodem.Registry
	Internet           *internet.Connector
	Reminders          *reminder.Scheduler
	LPA                *lpa.Pool
	OverviewExtensions []modemstatus.Extension
}

// Service owns modem observations and SIM lifecycle operations.
type Service struct {
	registry *mmodem.Registry
	catalog  *catalog
	simSlot  *simSlot
	msisdn   *msisdn
	settings *modemSettings
	internet *internet.Connector
}

const (
	switchSIMSlotTimeout = time.Minute
	updateMSISDNTimeout  = time.Minute
)

var (
	errSwitchSIMSlotTimeout = errors.New("switching SIM slot timed out, please refresh to confirm the active slot")
	errUpdateMSISDNTimeout  = errors.New("updating MSISDN timed out, please refresh to confirm the active slot")
)

// New creates the shared modem application service.
func New(cfg Config) *Service {
	catalog := newCatalog(cfg.Store, cfg.Registry, cfg.OverviewExtensions...)
	catalog.storage = cfg.Storage
	catalog.internet = cfg.Internet
	catalog.reminders = cfg.Reminders
	if cfg.LPA != nil {
		catalog.profiles = cfg.LPA
	}
	return &Service{
		registry: cfg.Registry,
		catalog:  catalog,
		simSlot:  newSIMSlot(cfg.Registry),
		msisdn:   newMSISDN(cfg.Registry),
		settings: newSettings(cfg.Store),
		internet: cfg.Internet,
	}
}

func (s *Service) switchSIM(ctx context.Context, device *mmodem.Modem, slot uint32) error {
	index, err := s.simSlot.targetIndex(device, slot)
	if errors.Is(err, errSIMSlotAlreadyActive) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, switchSIMSlotTimeout)
	defer cancel()
	if err := s.internet.Restore(ctx, device); err != nil {
		return err
	}
	return s.simSlot.Switch(ctx, device, index)
}

func (s *Service) updateNumber(ctx context.Context, device *mmodem.Modem, number string) error {
	ctx, cancel := context.WithTimeout(ctx, updateMSISDNTimeout)
	defer cancel()
	return s.msisdn.Update(ctx, device, number)
}
