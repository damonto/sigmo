package settings

import (
	"maps"
	"slices"
)

const (
	defaultProxyListenAddress = "127.0.0.1"
	defaultProxyHTTPPort      = 8080
	defaultProxySOCKS5Port    = 1080
	DefaultTokenValidityDays  = 30
	MaxTokenValidityDays      = 180
)

type Settings struct {
	Auth     Auth               `json:"auth"`
	Channels map[string]Channel `json:"channels"`
	MCP      MCP                `json:"mcp"`
	Modems   map[string]Modem   `json:"modems"`
	Proxy    *Proxy             `json:"proxy,omitempty"`
}

type MCP struct {
	Enabled bool `json:"enabled"`
}

type Auth struct {
	AuthProviders     []string `json:"authProviders"`
	OTPRequired       bool     `json:"otpRequired"`
	TokenValidityDays int      `json:"tokenValidityDays"`
}

type Channel struct {
	Enabled *bool `json:"enabled,omitempty"`

	Endpoint string `json:"endpoint,omitempty"`

	// Telegram
	BotToken   string     `json:"botToken,omitempty"`
	Recipients Recipients `json:"recipients,omitempty"`

	// HTTP
	Headers map[string]string `json:"headers,omitempty"`

	// Email
	SMTPHost     string `json:"smtpHost,omitempty"`
	SMTPPort     int    `json:"smtpPort,omitempty"`
	SMTPUsername string `json:"smtpUsername,omitempty"`
	SMTPPassword string `json:"smtpPassword,omitempty"`
	From         string `json:"from,omitempty"`
	TLSPolicy    string `json:"tlsPolicy,omitempty"`
	SSL          bool   `json:"ssl,omitempty"`

	// Gotify
	Priority int `json:"priority,omitempty"`
}

type Modem struct {
	Alias string `json:"alias"`
	MSS   int    `json:"mss"`
}

type Proxy struct {
	ListenAddress string `json:"listenAddress"`
	HTTPPort      int    `json:"httpPort"`
	SOCKS5Port    int    `json:"socks5Port"`
	Password      string `json:"password"`
}

func Default() *Settings {
	current := &Settings{}
	current.ApplyDefaults()
	return current
}

func DefaultModem() Modem {
	return Modem{
		MSS: 240,
	}
}

func DefaultProxy() Proxy {
	return Proxy{
		ListenAddress: defaultProxyListenAddress,
		HTTPPort:      defaultProxyHTTPPort,
		SOCKS5Port:    defaultProxySOCKS5Port,
	}
}

func (c *Settings) ApplyDefaults() {
	if c.Auth.AuthProviders == nil {
		c.Auth.AuthProviders = []string{}
	}
	if c.Auth.TokenValidityDays <= 0 {
		c.Auth.TokenValidityDays = DefaultTokenValidityDays
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

func (c *Settings) FindModem(id string) Modem {
	if modem, ok := c.Modems[id]; ok {
		return modem
	}
	return DefaultModem()
}

func (c *Settings) ProxySettings() Proxy {
	if c.Proxy == nil {
		return DefaultProxy()
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

func (c *Settings) Clone() Settings {
	clone := Settings{
		Auth: Auth{
			AuthProviders:     slices.Clone(c.Auth.AuthProviders),
			OTPRequired:       c.Auth.OTPRequired,
			TokenValidityDays: c.Auth.TokenValidityDays,
		},
		Channels: make(map[string]Channel, len(c.Channels)),
		MCP:      c.MCP,
		Modems:   maps.Clone(c.Modems),
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
