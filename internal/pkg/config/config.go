package config

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	defaultEnvironment        = "production"
	defaultListenAddress      = "0.0.0.0:9527"
	defaultProxyListenAddress = "127.0.0.1"
	defaultProxyHTTPPort      = 8080
	defaultProxySOCKS5Port    = 1080
	defaultConfigDirName      = "sigmo"
	defaultConfigFileName     = "config.toml"
)

// Config represents the application configuration
type Config struct {
	App      App                `toml:"app" json:"app"`
	Channels map[string]Channel `toml:"channels,omitempty" json:"channels"`
	Modems   map[string]Modem   `toml:"modems,omitempty" json:"modems"`
	Proxy    *Proxy             `toml:"proxy,omitempty" json:"proxy,omitempty"`
	Path     string             `toml:"-" json:"-"`
}

type App struct {
	Environment   string   `toml:"environment" json:"environment"`
	ListenAddress string   `toml:"listen_address" json:"listenAddress"`
	AuthProviders []string `toml:"auth_providers" json:"authProviders"`
	OTPRequired   bool     `toml:"otp_required" json:"otpRequired"`
}

type Channel struct {
	Enabled *bool `toml:"enabled,omitempty" json:"enabled,omitempty"`

	Endpoint string `toml:"endpoint,omitempty" json:"endpoint,omitempty"`

	// Telegram
	BotToken   string     `toml:"bot_token,omitempty" json:"botToken,omitempty"`
	Recipients Recipients `toml:"recipients,omitempty" json:"recipients,omitempty"`

	// HTTP
	Headers map[string]string `toml:"headers,omitempty" json:"headers,omitempty"`

	// Email
	SMTPHost     string `toml:"smtp_host,omitempty" json:"smtpHost,omitempty"`
	SMTPPort     int    `toml:"smtp_port,omitempty" json:"smtpPort,omitempty"`
	SMTPUsername string `toml:"smtp_username,omitempty" json:"smtpUsername,omitempty"`
	SMTPPassword string `toml:"smtp_password,omitempty" json:"smtpPassword,omitempty"`
	From         string `toml:"from,omitempty" json:"from,omitempty"`
	TLSPolicy    string `toml:"tls_policy,omitempty" json:"tlsPolicy,omitempty"`
	SSL          bool   `toml:"ssl,omitempty" json:"ssl,omitempty"`

	// Gotify
	Priority int `toml:"priority,omitempty" json:"priority,omitempty"`
}

type Modem struct {
	Alias      string `toml:"alias" json:"alias"`
	Compatible bool   `toml:"compatible" json:"compatible"`
	MSS        int    `toml:"mss" json:"mss"`
}

type Proxy struct {
	ListenAddress string `toml:"listen_address" json:"listenAddress"`
	HTTPPort      int    `toml:"http_port" json:"httpPort"`
	SOCKS5Port    int    `toml:"socks5_port" json:"socks5Port"`
	Password      string `toml:"password" json:"password"`
}

// Load reads and parses the configuration from the given file path
func Load(path string) (*Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("config path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()

	var config Config
	if err := toml.NewDecoder(file).DisallowUnknownFields().EnableUnmarshalerInterface().Decode(&config); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	config.Path = path
	config.ApplyDefaults()
	return &config, nil
}

func LoadOrCreate(path string) (*Config, error) {
	cfg, err := Load(path)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	cfg = Default()
	cfg.Path = path
	if err := cfg.Save(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(dir, defaultConfigDirName, defaultConfigFileName), nil
}

func Default() *Config {
	cfg := &Config{}
	cfg.ApplyDefaults()
	return cfg
}

func (c *Config) ApplyDefaults() {
	if c.App.Environment == "" {
		c.App.Environment = defaultEnvironment
	}
	if c.App.ListenAddress == "" {
		c.App.ListenAddress = defaultListenAddress
	}
	if c.App.AuthProviders == nil {
		c.App.AuthProviders = []string{}
	}
	if c.Channels == nil {
		c.Channels = map[string]Channel{}
	}
	for name, channel := range c.Channels {
		if channel.Enabled == nil {
			enabled := true
			channel.Enabled = &enabled
			c.Channels[name] = channel
		}
	}
	if c.Modems == nil {
		c.Modems = map[string]Modem{}
	}
}

func (c *Config) IsProduction() bool {
	return c.App.Environment == "production"
}

func (c *Config) FindModem(id string) Modem {
	if modem, ok := c.Modems[id]; ok {
		return modem
	}
	return Modem{
		Compatible: false,
		MSS:        240,
	}
}

func (c *Config) ProxySettings() Proxy {
	if c.Proxy == nil {
		return Proxy{
			ListenAddress: defaultProxyListenAddress,
			HTTPPort:      defaultProxyHTTPPort,
			SOCKS5Port:    defaultProxySOCKS5Port,
		}
	}
	proxy := *c.Proxy
	if proxy.ListenAddress == "" {
		proxy.ListenAddress = defaultProxyListenAddress
	}
	if proxy.HTTPPort == 0 {
		proxy.HTTPPort = defaultProxyHTTPPort
	}
	if proxy.SOCKS5Port == 0 {
		proxy.SOCKS5Port = defaultProxySOCKS5Port
	}
	return proxy
}

func (c *Config) Clone() Config {
	clone := Config{
		App: App{
			Environment:   c.App.Environment,
			ListenAddress: c.App.ListenAddress,
			AuthProviders: slices.Clone(c.App.AuthProviders),
			OTPRequired:   c.App.OTPRequired,
		},
		Channels: make(map[string]Channel, len(c.Channels)),
		Modems:   maps.Clone(c.Modems),
		Path:     c.Path,
	}
	if c.Proxy != nil {
		proxy := *c.Proxy
		clone.Proxy = &proxy
	}
	for name, channel := range c.Channels {
		clone.Channels[name] = channel.Clone()
	}
	return clone
}

func (c Channel) Clone() Channel {
	clone := c
	if c.Enabled != nil {
		enabled := *c.Enabled
		clone.Enabled = &enabled
	}
	clone.Recipients = slices.Clone(c.Recipients)
	clone.Headers = maps.Clone(c.Headers)
	return clone
}

func (c Channel) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

func (c *Config) Save() error {
	if c.Path == "" {
		return errors.New("config path is required")
	}
	c.ApplyDefaults()
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tempFile, err := os.CreateTemp(dir, filepath.Base(c.Path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create config temp file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := tempFile.Write(buf.Bytes()); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write config temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close config temp file: %w", err)
	}
	if err := os.Rename(tempPath, c.Path); err != nil {
		return fmt.Errorf("replace config file: %w", err)
	}
	cleanup = false
	return nil
}
