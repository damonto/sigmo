package wificalling

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/storage"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const (
	FeatureName = "wifiCalling"

	scopePrefix          = "profile:"
	keyEnabled           = "wifi_calling.enabled"
	keyPreferred         = "wifi_calling.preferred"
	actionUSSDInitialize = "initialize"
	actionUSSDReply      = "reply"
)

var (
	ErrUnavailable  = errors.New("wifi calling is unavailable")
	ErrNotConnected = errors.New("wifi calling is not connected")
)

type Settings struct {
	Enabled   bool
	Preferred bool
}

type Status struct {
	Settings
	Connected bool
}

type IncomingSMS struct {
	ModemID string
	Message storage.Message
}

type IncomingSMSFunc func(context.Context, IncomingSMS) error

type Coordinator interface {
	Run(context.Context, *mmodem.Registry) error
	Settings(context.Context, *mmodem.Modem) (Settings, error)
	UpdateSettings(context.Context, *mmodem.Modem, Settings) error
	Status(context.Context, *mmodem.Modem) (Status, error)
	SendSMS(context.Context, *mmodem.Modem, string, string) (storage.Message, error)
	ExecuteUSSD(context.Context, *mmodem.Modem, string, string) (string, error)
}

type SettingsStore struct {
	store *storage.Store
}

func NewSettingsStore(store *storage.Store) *SettingsStore {
	return &SettingsStore{store: store}
}

func (s *SettingsStore) Get(ctx context.Context, profileID string) (Settings, error) {
	if s == nil || s.store == nil {
		return Settings{}, nil
	}
	scope, err := profileScope(profileID)
	if err != nil {
		return Settings{}, err
	}
	var settings Settings
	if err := s.store.Get(ctx, scope, keyEnabled, &settings.Enabled); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return Settings{}, fmt.Errorf("read wifi calling enabled: %w", err)
	}
	if err := s.store.Get(ctx, scope, keyPreferred, &settings.Preferred); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return Settings{}, fmt.Errorf("read wifi calling preference: %w", err)
	}
	return settings, nil
}

func (s *SettingsStore) Put(ctx context.Context, profileID string, settings Settings) error {
	if s == nil || s.store == nil {
		return nil
	}
	scope, err := profileScope(profileID)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		settings.Preferred = false
	}
	if err := s.store.Put(ctx, scope, keyEnabled, settings.Enabled); err != nil {
		return fmt.Errorf("save wifi calling enabled: %w", err)
	}
	if err := s.store.Put(ctx, scope, keyPreferred, settings.Preferred); err != nil {
		return fmt.Errorf("save wifi calling preference: %w", err)
	}
	return nil
}

func profileScope(profileID string) (string, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return "", mmodem.ErrProfileIDMissing
	}
	return scopePrefix + profileID, nil
}
