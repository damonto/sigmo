package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config represents the application configuration
type Config struct {
	App      App                `toml:"app"`
	Channels map[string]Channel `toml:"channels"`
	Modems   map[string]Modem   `toml:"modems"`
	Path     string             `toml:"-"`
}

type App struct {
	Environment   string   `toml:"environment"`
	ListenAddress string   `toml:"listen_address"`
	AuthProviders []string `toml:"auth_providers"`
	OTPRequired   bool     `toml:"otp_required"`
}

type Channel struct {
	Endpoint string `toml:"endpoint"`

	// Telegram
	BotToken   string     `toml:"bot_token"`
	Recipients Recipients `toml:"recipients"`

	// HTTP
	Headers map[string]string `toml:"headers"`

	// Email
	SMTPHost     string `toml:"smtp_host"`
	SMTPPort     int    `toml:"smtp_port"`
	SMTPUsername string `toml:"smtp_username"`
	SMTPPassword string `toml:"smtp_password"`
	From         string `toml:"from"`
	Subject      string `toml:"subject"`
	TLSPolicy    string `toml:"tls_policy"`
	SSL          bool   `toml:"ssl"`

	// Gotify
	Priority int    `toml:"priority"`
}

type Modem struct {
	Alias      string `toml:"alias"`
	Compatible bool   `toml:"compatible"`
	MSS        int    `toml:"mss"`
}

// Load reads and parses the configuration from the given file path
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	var config Config
	meta, err := toml.Decode(string(data), &config)
	if err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}
	if !meta.IsDefined("app", "otp_required") {
		config.App.OTPRequired = true
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
