package settings

import "time"

import appsettings "github.com/damonto/sigmo/internal/pkg/settings"

type Response struct {
	Schema Schema `json:"schema"`
	Values Values `json:"values"`
}

type Values struct {
	Notifications appsettings.Notifications `json:"notifications"`
	Auth          AuthValues                `json:"auth" validate:"required"`
	Proxy         ProxyValues               `json:"proxy" validate:"required"`
	Channels      map[string]ChannelValues  `json:"channels" validate:"omitempty,dive"`
}

// AuthValues flattens the nested policy for the schema-driven settings editors.
type AuthValues struct {
	Mode                  string   `json:"mode"`
	SessionMaxDays        int      `json:"sessionMaxDays"`
	OTPChannels           []string `json:"otpChannels"`
	OIDCIssuer            string   `json:"oidcIssuer"`
	OIDCClientID          string   `json:"oidcClientId"`
	OIDCClientSecret      string   `json:"oidcClientSecret"`
	OIDCAllowedSubjects   []string `json:"oidcAllowedSubjects"`
	AccessTeamDomain      string   `json:"accessTeamDomain"`
	AccessAudience        string   `json:"accessAudience"`
	AccessAllowedSubjects []string `json:"accessAllowedSubjects"`
}

// UpdateAuthRequest carries a complete policy; an empty secret retains the stored secret.
type UpdateAuthRequest AuthValues

// AuthTestRequest selects delivery destinations without changing their other uses.
type AuthTestRequest struct {
	Channels []string `json:"channels"`
}

// OIDCTestSessionRequest creates a temporary browser login from unsaved values.
type OIDCTestSessionRequest struct {
	Client   string     `json:"client"`
	BasePath string     `json:"basePath"`
	State    string     `json:"state"`
	Auth     AuthValues `json:"auth"`
}

type OIDCTestSessionResponse struct {
	ID               string    `json:"id"`
	AuthorizationURL string    `json:"authorizationUrl"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

type ProxyValues struct {
	ListenAddress string `json:"listenAddress" validate:"required"`
	HTTPPort      int    `json:"httpPort" validate:"gte=1,lte=65535"`
	SOCKS5Port    int    `json:"socks5Port" validate:"gte=1,lte=65535"`
	Password      string `json:"password"`
}

type ChannelValues struct {
	Endpoint string `json:"endpoint,omitempty" validate:"omitempty,http_url"`

	BotToken   string   `json:"botToken,omitempty"`
	Recipients []string `json:"recipients,omitempty" validate:"omitempty,dive,required"`

	Headers map[string]string `json:"headers,omitempty" validate:"omitempty,dive,keys,required,endkeys"`

	SMTPHost     string `json:"smtpHost,omitempty"`
	SMTPPort     int    `json:"smtpPort,omitempty" validate:"omitempty,gte=1,lte=65535"`
	SMTPUsername string `json:"smtpUsername,omitempty"`
	SMTPPassword string `json:"smtpPassword,omitempty"`
	From         string `json:"from,omitempty"`
	TLSPolicy    string `json:"tlsPolicy,omitempty" validate:"omitempty,oneof=mandatory opportunistic none notls no_tls"`
	SSL          bool   `json:"ssl,omitempty"`

	Priority int `json:"priority,omitempty" validate:"gte=0,lte=10"`
}

type Schema struct {
	Auth     []Field         `json:"auth"`
	Proxy    []Field         `json:"proxy"`
	Channels []ChannelSchema `json:"channels"`
}

type ChannelSchema struct {
	Key         string  `json:"key"`
	Label       string  `json:"label"`
	Description string  `json:"description,omitempty"`
	Fields      []Field `json:"fields"`
}

type Field struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Control     string   `json:"control"`
	Required    bool     `json:"required,omitempty"`
	Secret      bool     `json:"secret,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Min         *int     `json:"min,omitempty"`
	Max         *int     `json:"max,omitempty"`
	Options     []Option `json:"options,omitempty"`
}

type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
