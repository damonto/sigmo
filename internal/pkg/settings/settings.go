package settings

import (
	"maps"
	"slices"
)

const (
	defaultProxyListenAddress = "127.0.0.1"
	defaultProxyHTTPPort      = 8080
	defaultProxySOCKS5Port    = 1080
	DefaultSessionMaxDays     = 30
	MaxSessionMaxDays         = 180
	UpdateChannelStable       = "stable"
	UpdateChannelDev          = "dev"
)

type Settings struct {
	Auth          Auth               `json:"auth"`
	Notifications Notifications      `json:"notifications"`
	Channels      map[string]Channel `json:"channels"`
	MCP           MCP                `json:"mcp"`
	Modems        map[string]Modem   `json:"modems"`
	Proxy         *Proxy             `json:"proxy,omitempty"`
	Updates       Updates            `json:"updates"`
}

type MCP struct {
	Enabled bool `json:"enabled"`
}

type Updates struct {
	Automatic bool   `json:"automatic"`
	Channel   string `json:"channel"`
}

// Notifications selects channels for business events independently of authentication.
type Notifications struct {
	Channels []string `json:"channels"`
}

type Channel struct {
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
	c.Auth.ApplyDefaults()
	if c.Notifications.Channels == nil {
		c.Notifications.Channels = []string{}
	}
	if c.Channels == nil {
		c.Channels = map[string]Channel{}
	}
	if c.Modems == nil {
		c.Modems = map[string]Modem{}
	}
	if c.Updates.Channel != UpdateChannelDev {
		c.Updates.Channel = UpdateChannelStable
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
		Auth:          c.Auth.Clone(),
		Notifications: Notifications{Channels: slices.Clone(c.Notifications.Channels)},
		Channels:      make(map[string]Channel, len(c.Channels)),
		MCP:           c.MCP,
		Modems:        maps.Clone(c.Modems),
		Updates:       c.Updates,
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
	clone.Recipients = slices.Clone(c.Recipients)
	clone.Headers = maps.Clone(c.Headers)
	return clone
}
