// Package esim coordinates eSIM inventory, provisioning, and profile lifecycle.
package esim

import (
	"context"
	"errors"
	"time"

	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// Config supplies the shared dependencies required by every eSIM entry point.
type Config struct {
	Store     *settings.Store
	Registry  *mmodem.Registry
	Internet  *internet.Connector
	LPA       *lpa.Pool
	Reminders *reminder.Scheduler
	Storage   *storage.Store
}

type modemFinder interface {
	Find(context.Context, string) (*mmodem.Modem, error)
}

// Service coordinates inventory and lifecycle side effects for every transport.
type Service struct {
	registry     modemFinder
	profile      *profile
	provisioning *provisioning
	lifecycle    *lifecycle
	internet     *internet.Connector
	reminders    *reminder.Scheduler
}

// Some Qualcomm firmware needs nearly five minutes to expose an enabled profile.
const enableTimeout = 5 * time.Minute

// New creates the shared eSIM application service.
func New(cfg Config) (*Service, error) {
	if cfg.Store == nil || cfg.Registry == nil || cfg.Internet == nil || cfg.LPA == nil || cfg.Reminders == nil || cfg.Storage == nil {
		return nil, errors.New("eSIM service dependencies are required")
	}
	profiles := newProfile(cfg.Store, cfg.Reminders, cfg.LPA)
	profiles.history = cfg.Storage
	return &Service{
		registry:     cfg.Registry,
		profile:      profiles,
		provisioning: newProvisioning(cfg.LPA),
		lifecycle:    newLifecycle(cfg.Store, cfg.Registry, cfg.LPA),
		internet:     cfg.Internet,
		reminders:    cfg.Reminders,
	}, nil
}

func (s *Service) restoreInternetBeforeProfileEnable(ctx context.Context, modem *mmodem.Modem) error {
	if modem != nil && modem.Snapshot().Locked() {
		return nil
	}
	if s.internet == nil {
		return errors.New("internet connector is required")
	}
	return s.internet.Restore(ctx, modem)
}
