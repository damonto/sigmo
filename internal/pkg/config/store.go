package config

import "sync"

type Store struct {
	mu  sync.RWMutex
	cfg Config
}

func NewStore(cfg *Config) *Store {
	clone := cfg.Clone()
	clone.ApplyDefaults()
	return &Store{cfg: clone}
}

func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Clone()
}

func (s *Store) IsProduction() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.IsProduction()
}

func (s *Store) OTPRequired() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.App.OTPRequired
}

func (s *Store) FindModem(id string) Modem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.FindModem(id)
}

func (s *Store) ProxySettings() Proxy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.ProxySettings()
}

func (s *Store) Update(update func(*Config) error) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.cfg.Clone()
	if err := update(&next); err != nil {
		return Config{}, err
	}
	next.Path = s.cfg.Path
	if err := next.Save(); err != nil {
		return Config{}, err
	}
	s.cfg = next.Clone()
	return s.cfg.Clone(), nil
}

func (s *Store) UpdateModem(id string, modem Modem) error {
	_, err := s.Update(func(cfg *Config) error {
		cfg.Modems[id] = modem
		return nil
	})
	return err
}
