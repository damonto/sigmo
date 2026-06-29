package settings

type Response struct {
	Schema Schema `json:"schema"`
	Values Values `json:"values"`
}

type Values struct {
	Auth     AuthValues               `json:"auth" validate:"required"`
	Proxy    ProxyValues              `json:"proxy" validate:"required"`
	Channels map[string]ChannelValues `json:"channels" validate:"omitempty,dive"`
}

type AuthValues struct {
	AuthProviders []string `json:"authProviders" validate:"omitempty,dive,required"`
	OTPRequired   bool     `json:"otpRequired"`
}

type ProxyValues struct {
	ListenAddress string `json:"listenAddress" validate:"required"`
	HTTPPort      int    `json:"httpPort" validate:"gte=1,lte=65535"`
	SOCKS5Port    int    `json:"socks5Port" validate:"gte=1,lte=65535"`
	Password      string `json:"password"`
}

type ChannelValues struct {
	Enabled *bool `json:"enabled,omitempty" validate:"required"`

	Endpoint string `json:"endpoint,omitempty" validate:"omitempty,http_url"`

	BotToken   string   `json:"botToken,omitempty"`
	Recipients []string `json:"recipients,omitempty" validate:"omitempty,dive,required"`

	Headers map[string]string `json:"headers,omitempty" validate:"omitempty,dive,keys,required,endkeys"`
	Format  string            `json:"format,omitempty" validate:"omitempty,oneof=raw feishu wecom"`

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

type UpdateRequest = Values
