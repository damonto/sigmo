package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/damonto/sigmo/internal/pkg/locale"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	globalScopeKey           = "global"
	authSettingsKey          = "auth.settings"
	channelSettingsKey       = "delivery.channels"
	notificationsSettingsKey = "notification.settings"
	mcpSettingsKey           = "mcp.settings"
	proxySettingsKey         = "proxy.settings"
	modemSettingsKey         = "modem.settings"
	modemSettingsPrefix      = "modem:"
	updateSettingsKey        = "update.settings"
	localeKey                = "ui.locale"
)

var errStorageRequired = errors.New("settings storage is required")

type Store struct {
	mu                sync.RWMutex
	db                *storage.Store
	current           Settings
	memory            bool
	updatesConfigured bool
	// locale lives beside current rather than in Settings: it is observed
	// from requests, not configured, so Update must never overwrite it.
	locale locale.Tag
}

func NewStore(ctx context.Context, db *storage.Store) (*Store, error) {
	if db == nil {
		return nil, errStorageRequired
	}
	current, updatesConfigured, err := load(ctx, db)
	if err != nil {
		return nil, err
	}
	var lang locale.Tag
	if _, err := get(ctx, db, globalScopeKey, localeKey, &lang); err != nil {
		return nil, fmt.Errorf("load locale: %w", err)
	}
	return &Store{
		db:                db,
		current:           current,
		updatesConfigured: updatesConfigured,
		locale:            locale.Parse(string(lang)),
	}, nil
}

func NewMemoryStore(current *Settings) *Store {
	clone := current.Clone()
	updatesConfigured := clone.Updates.Channel != ""
	clone.ApplyDefaults()
	return &Store{current: clone, memory: true, updatesConfigured: updatesConfigured}
}

func (s *Store) Snapshot() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Clone()
}

func (s *Store) FindModem(id string) Modem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.FindModem(id)
}

func (s *Store) ProxySettings() Proxy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.ProxySettings()
}

func (s *Store) MCPSettings() MCP {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.MCP
}

func (s *Store) UpdateSettings() (Updates, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Updates, s.updatesConfigured
}

// Locale is the web UI language of the most recent authenticated request.
// Notifications use it because events such as an incoming SMS have no
// request of their own to take a language from.
func (s *Store) Locale() locale.Tag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.locale
}

// SetLocale records lang as the most recent web UI language. It writes only
// when the language changes, because it runs on every authenticated request.
func (s *Store) SetLocale(ctx context.Context, lang locale.Tag) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if lang == s.locale {
		return nil
	}
	if err := s.putLocale(ctx, lang); err != nil {
		return fmt.Errorf("save locale: %w", err)
	}
	s.locale = lang
	return nil
}

func (s *Store) Update(ctx context.Context, update func(*Settings) error) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.current.Clone()
	if err := update(&next); err != nil {
		return Settings{}, err
	}
	next.ApplyDefaults()
	if err := next.Validate(); err != nil {
		return Settings{}, err
	}
	if err := s.save(ctx, next, s.updatesConfigured); err != nil {
		return Settings{}, err
	}
	s.current = next.Clone()
	return s.current.Clone(), nil
}

func (s *Store) SetUpdateSettings(ctx context.Context, updates Updates) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.current.Clone()
	next.Updates = updates
	next.ApplyDefaults()
	if err := next.Validate(); err != nil {
		return Settings{}, err
	}
	if err := s.save(ctx, next, true); err != nil {
		return Settings{}, err
	}
	s.current = next.Clone()
	s.updatesConfigured = true
	return s.current.Clone(), nil
}

func (s *Store) UpdateModem(ctx context.Context, id string, modem Modem) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("modem id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.putModem(ctx, id, modem); err != nil {
		return err
	}
	s.current.Modems[id] = modem
	return nil
}

func load(ctx context.Context, db *storage.Store) (Settings, bool, error) {
	current := Default().Clone()

	var auth Auth
	ok, err := get(ctx, db, globalScopeKey, authSettingsKey, &auth)
	if err != nil {
		return Settings{}, false, err
	}
	if ok {
		if auth.Mode == "" {
			return Settings{}, false, errors.New("authentication mode is missing")
		}
		current.Auth = auth
	}

	var channels map[string]Channel
	ok, err = get(ctx, db, globalScopeKey, channelSettingsKey, &channels)
	if err != nil {
		return Settings{}, false, err
	}
	if ok {
		current.Channels = channels
	}

	if _, err := get(ctx, db, globalScopeKey, notificationsSettingsKey, &current.Notifications); err != nil {
		return Settings{}, false, err
	}
	var proxy Proxy
	ok, err = get(ctx, db, globalScopeKey, proxySettingsKey, &proxy)
	if err != nil {
		return Settings{}, false, err
	}
	if ok {
		current.Proxy = &proxy
	}

	var mcp MCP
	ok, err = get(ctx, db, globalScopeKey, mcpSettingsKey, &mcp)
	if err != nil {
		return Settings{}, false, err
	}
	if ok {
		current.MCP = mcp
	}

	var updates Updates
	updatesConfigured, err := get(ctx, db, globalScopeKey, updateSettingsKey, &updates)
	if err != nil {
		return Settings{}, false, err
	}
	if updatesConfigured {
		current.Updates = updates
	}

	modems, err := loadModems(ctx, db)
	if err != nil {
		return Settings{}, false, err
	}
	current.Modems = modems
	current.ApplyDefaults()
	if err := current.Validate(); err != nil {
		return Settings{}, false, fmt.Errorf("validate settings: %w", err)
	}
	return current, updatesConfigured, nil
}

func loadModems(ctx context.Context, db *storage.Store) (map[string]Modem, error) {
	raw, err := db.ListRaw(ctx, modemSettingsPrefix, modemSettingsKey)
	if err != nil {
		return nil, err
	}
	modems := make(map[string]Modem, len(raw))
	for scope, value := range raw {
		id := strings.TrimPrefix(scope, modemSettingsPrefix)
		if strings.TrimSpace(id) == "" {
			continue
		}
		var modem Modem
		if err := json.Unmarshal([]byte(value), &modem); err != nil {
			return nil, fmt.Errorf("decode modem settings for %s: %w", scope, err)
		}
		modems[id] = modem
	}
	return modems, nil
}

func (s *Store) save(ctx context.Context, current Settings, updatesConfigured bool) error {
	if s.memory {
		return nil
	}
	if s.db == nil {
		return errStorageRequired
	}
	values := map[string]json.RawMessage{}
	entries := []struct {
		key   string
		value any
	}{
		{authSettingsKey, current.Auth}, {channelSettingsKey, current.Channels},
		{notificationsSettingsKey, current.Notifications}, {mcpSettingsKey, current.MCP},
		{proxySettingsKey, current.ProxySettings()},
	}
	if updatesConfigured {
		entries = append(entries, struct {
			key   string
			value any
		}{updateSettingsKey, current.Updates})
	}
	for _, entry := range entries {
		data, err := json.Marshal(entry.value)
		if err != nil {
			return fmt.Errorf("encode %s: %w", entry.key, err)
		}
		values[entry.key] = data
	}
	return s.db.PutBatch(ctx, globalScopeKey, values)
}

func (s *Store) putModem(ctx context.Context, id string, modem Modem) error {
	if s.memory {
		return nil
	}
	if s.db == nil {
		return errStorageRequired
	}
	return s.db.Put(ctx, modemSettingsPrefix+id, modemSettingsKey, modem)
}

func (s *Store) putLocale(ctx context.Context, lang locale.Tag) error {
	if s.memory {
		return nil
	}
	if s.db == nil {
		return errStorageRequired
	}
	return s.db.Put(ctx, globalScopeKey, localeKey, lang)
}

func get(ctx context.Context, db *storage.Store, scope string, key string, dst any) (bool, error) {
	err := db.Get(ctx, scope, key, dst)
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
