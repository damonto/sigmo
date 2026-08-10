package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	globalScopeKey      = "global"
	authSettingsKey     = "auth.settings"
	channelSettingsKey  = "notification.channels"
	mcpSettingsKey      = "mcp.settings"
	proxySettingsKey    = "proxy.settings"
	modemSettingsKey    = "modem.settings"
	modemSettingsPrefix = "modem:"
	updateSettingsKey   = "update.settings"
)

var errStorageRequired = errors.New("settings storage is required")

type Store struct {
	mu                sync.RWMutex
	db                *storage.Store
	current           Settings
	memory            bool
	updatesConfigured bool
}

func NewStore(ctx context.Context, db *storage.Store) (*Store, error) {
	if db == nil {
		return nil, errStorageRequired
	}
	current, updatesConfigured, err := load(ctx, db)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, current: current, updatesConfigured: updatesConfigured}, nil
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

func (s *Store) OTPRequired() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Auth.OTPRequired
}

func (s *Store) TokenValidityDays() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Auth.TokenValidityDays
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

func (s *Store) Update(ctx context.Context, update func(*Settings) error) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.current.Clone()
	if err := update(&next); err != nil {
		return Settings{}, err
	}
	next.ApplyDefaults()
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
	if ok, err := get(ctx, db, globalScopeKey, authSettingsKey, &auth); err != nil {
		return Settings{}, false, err
	} else if ok {
		current.Auth = auth
	}

	var channels map[string]Channel
	if ok, err := get(ctx, db, globalScopeKey, channelSettingsKey, &channels); err != nil {
		return Settings{}, false, err
	} else if ok {
		current.Channels = channels
	}

	var proxy Proxy
	if ok, err := get(ctx, db, globalScopeKey, proxySettingsKey, &proxy); err != nil {
		return Settings{}, false, err
	} else if ok {
		current.Proxy = &proxy
	}

	var mcp MCP
	if ok, err := get(ctx, db, globalScopeKey, mcpSettingsKey, &mcp); err != nil {
		return Settings{}, false, err
	} else if ok {
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
	if err := s.db.Put(ctx, globalScopeKey, authSettingsKey, current.Auth); err != nil {
		return err
	}
	if err := s.db.Put(ctx, globalScopeKey, channelSettingsKey, current.Channels); err != nil {
		return err
	}
	if err := s.db.Put(ctx, globalScopeKey, mcpSettingsKey, current.MCP); err != nil {
		return err
	}
	if updatesConfigured {
		if err := s.db.Put(ctx, globalScopeKey, updateSettingsKey, current.Updates); err != nil {
			return err
		}
	}
	return s.db.Put(ctx, globalScopeKey, proxySettingsKey, current.ProxySettings())
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
