package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

const (
	defaultProxyListenAddress = "127.0.0.1"
	defaultProxyHTTPPort      = 8080
	defaultProxySOCKS5Port    = 1080
)

// Config represents the application configuration
type Config struct {
	App      App                `toml:"app"`
	Channels map[string]Channel `toml:"channels"`
	Modems   map[string]Modem   `toml:"modems"`
	Proxy    *Proxy             `toml:"proxy,omitempty"`
	Path     string             `toml:"-"`
}

type App struct {
	Environment   string   `toml:"environment"`
	ListenAddress string   `toml:"listen_address"`
	AuthProviders []string `toml:"auth_providers"`
	OTPRequired   bool     `toml:"otp_required"`
}

type Channel struct {
	Endpoint string `toml:"endpoint,omitempty"`

	// Telegram
	BotToken   string     `toml:"bot_token,omitempty"`
	Recipients Recipients `toml:"recipients,omitempty"`

	// HTTP
	Headers map[string]string `toml:"headers,omitempty"`

	// Email
	SMTPHost     string `toml:"smtp_host,omitempty"`
	SMTPPort     int    `toml:"smtp_port,omitempty"`
	SMTPUsername string `toml:"smtp_username,omitempty"`
	SMTPPassword string `toml:"smtp_password,omitempty"`
	From         string `toml:"from,omitempty"`
	TLSPolicy    string `toml:"tls_policy,omitempty"`
	SSL          bool   `toml:"ssl,omitempty"`

	// Gotify
	Priority int `toml:"priority,omitempty"`
}

type Modem struct {
	Alias      string `toml:"alias"`
	Compatible bool   `toml:"compatible"`
	MSS        int    `toml:"mss"`
}

type Proxy struct {
	ListenAddress string `toml:"listen_address"`
	HTTPPort      int    `toml:"http_port"`
	SOCKS5Port    int    `toml:"socks5_port"`
	Password      string `toml:"password"`
}

// Load reads and parses the configuration from the given file path
func Load(path string) (*Config, error) {
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
	return &config, nil
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

func (c *Config) Save() error {
	if c.Path == "" {
		return errors.New("config path is required")
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return fmt.Errorf("encoding config file: %w", err)
	}
	if err := os.WriteFile(c.Path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	return nil
}
