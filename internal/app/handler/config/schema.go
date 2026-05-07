package config

const (
	controlText        = "text"
	controlPassword    = "password"
	controlNumber      = "number"
	controlSwitch      = "switch"
	controlSelect      = "select"
	controlList        = "list"
	controlKeyValue    = "keyValue"
	controlChannelList = "channelList"
)

func configSchema() Schema {
	return Schema{
		App: []Field{
			selectField("environment", "Environment", "Runtime mode.", true, []Option{
				{Label: "Production", Value: "production"},
				{Label: "Development", Value: "development"},
			}),
			textField("listenAddress", "Listen address", "HTTP bind address. Changes require restart.", "0.0.0.0:9527", true),
			{
				Key:         "otpRequired",
				Label:       "OTP required",
				Description: "Require one-time password verification before using the API.",
				Control:     controlSwitch,
			},
			{
				Key:         "authProviders",
				Label:       "Auth providers",
				Description: "Enabled channels allowed to send login OTPs.",
				Control:     controlChannelList,
			},
		},
		Proxy: []Field{
			textField("listenAddress", "Listen address", "Proxy listener address.", "127.0.0.1", true),
			numberField("httpPort", "HTTP port", "HTTP proxy port.", 1, 65535, true),
			numberField("socks5Port", "SOCKS5 port", "SOCKS5 proxy port.", 1, 65535, true),
			passwordField("password", "Password", "Proxy password used with the modem interface name as username.", false),
		},
		Channels: []ChannelSchema{
			{
				Key:         "telegram",
				Label:       "Telegram",
				Description: "Send OTP and SMS notifications with a Telegram bot.",
				Fields: []Field{
					textField("endpoint", "Endpoint", "Telegram API endpoint. Leave empty for the official API.", "https://api.telegram.org", false),
					passwordField("botToken", "Bot token", "Token from BotFather.", true),
					listField("recipients", "Recipients", "Add one Telegram chat ID per tag.", true),
				},
			},
			{
				Key:         "bark",
				Label:       "Bark",
				Description: "Send notifications to Bark on iOS.",
				Fields: []Field{
					textField("endpoint", "Endpoint", "Bark server URL. Leave empty for the official API.", "https://api.day.app", false),
					listField("recipients", "Recipients", "Add one device key per tag.", true),
				},
			},
			{
				Key:         "gotify",
				Label:       "Gotify",
				Description: "Send notifications to a Gotify server.",
				Fields: []Field{
					textField("endpoint", "Endpoint", "Gotify server base URL.", "https://push.example.com", true),
					listField("recipients", "Application tokens", "Add one Gotify application token per tag.", true),
					numberField("priority", "Priority", "Message priority.", 0, 10, false),
				},
			},
			{
				Key:         "sc3",
				Label:       "ServerChan",
				Description: "Send notifications with a ServerChan sendkey URL.",
				Fields: []Field{
					textField("endpoint", "Endpoint", "Full ServerChan sendkey URL.", "https://uid.push.ft07.com/send/key.send", true),
				},
			},
			{
				Key:         "http",
				Label:       "HTTP webhook",
				Description: "POST notification events to a custom webhook.",
				Fields: []Field{
					textField("endpoint", "Endpoint", "Webhook URL.", "https://example.com/webhook", true),
					{
						Key:         "headers",
						Label:       "Headers",
						Description: "Optional HTTP headers.",
						Control:     controlKeyValue,
					},
				},
			},
			{
				Key:         "email",
				Label:       "Email",
				Description: "Send notifications through SMTP.",
				Fields: []Field{
					textField("smtpHost", "SMTP host", "SMTP server hostname.", "smtp.example.com", true),
					numberField("smtpPort", "SMTP port", "SMTP server port.", 1, 65535, true),
					textField("smtpUsername", "SMTP username", "SMTP username.", "user@example.com", false),
					passwordField("smtpPassword", "SMTP password", "SMTP password or app password.", false),
					textField("from", "From", "Sender address.", "Sigmo <sigmo@example.com>", true),
					listField("recipients", "Recipients", "Add one recipient email per tag.", true),
					selectField("tlsPolicy", "TLS policy", "STARTTLS policy.", false, []Option{
						{Label: "Mandatory", Value: "mandatory"},
						{Label: "Opportunistic", Value: "opportunistic"},
						{Label: "None", Value: "none"},
					}),
					{
						Key:         "ssl",
						Label:       "SSL",
						Description: "Use implicit SSL, usually for port 465.",
						Control:     controlSwitch,
					},
				},
			},
		},
	}
}

func textField(key string, label string, description string, placeholder string, required bool) Field {
	return Field{
		Key:         key,
		Label:       label,
		Description: description,
		Control:     controlText,
		Placeholder: placeholder,
		Required:    required,
	}
}

func passwordField(key string, label string, description string, required bool) Field {
	return Field{
		Key:         key,
		Label:       label,
		Description: description,
		Control:     controlPassword,
		Required:    required,
		Secret:      true,
	}
}

func listField(key string, label string, description string, required bool) Field {
	return Field{
		Key:         key,
		Label:       label,
		Description: description,
		Control:     controlList,
		Required:    required,
	}
}

func numberField(key string, label string, description string, minValue int, maxValue int, required bool) Field {
	return Field{
		Key:         key,
		Label:       label,
		Description: description,
		Control:     controlNumber,
		Min:         &minValue,
		Max:         &maxValue,
		Required:    required,
	}
}

func selectField(key string, label string, description string, required bool, options []Option) Field {
	return Field{
		Key:         key,
		Label:       label,
		Description: description,
		Control:     controlSelect,
		Required:    required,
		Options:     options,
	}
}
