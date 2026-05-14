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
			selectField("environment", "config.schema.app.environment.label", "config.schema.app.environment.description", true, []Option{
				{Label: "config.schema.app.environment.options.production", Value: "production"},
				{Label: "config.schema.app.environment.options.development", Value: "development"},
			}),
			textField("listenAddress", "config.schema.app.listenAddress.label", "config.schema.app.listenAddress.description", "config.schema.app.listenAddress.placeholder", true),
			{
				Key:         "otpRequired",
				Label:       "config.schema.app.otpRequired.label",
				Description: "config.schema.app.otpRequired.description",
				Control:     controlSwitch,
			},
			{
				Key:         "authProviders",
				Label:       "config.schema.app.authProviders.label",
				Description: "config.schema.app.authProviders.description",
				Control:     controlChannelList,
			},
		},
		Proxy: []Field{
			textField("listenAddress", "config.schema.proxy.listenAddress.label", "config.schema.proxy.listenAddress.description", "config.schema.proxy.listenAddress.placeholder", true),
			numberField("httpPort", "config.schema.proxy.httpPort.label", "config.schema.proxy.httpPort.description", 1, 65535, true),
			numberField("socks5Port", "config.schema.proxy.socks5Port.label", "config.schema.proxy.socks5Port.description", 1, 65535, true),
			passwordField("password", "config.schema.proxy.password.label", "config.schema.proxy.password.description", false),
		},
		Channels: []ChannelSchema{
			{
				Key:         "telegram",
				Label:       "config.schema.channels.telegram.label",
				Description: "config.schema.channels.telegram.description",
				Fields: []Field{
					textField("endpoint", "config.schema.channels.telegram.endpoint.label", "config.schema.channels.telegram.endpoint.description", "config.schema.channels.telegram.endpoint.placeholder", false),
					passwordField("botToken", "config.schema.channels.telegram.botToken.label", "config.schema.channels.telegram.botToken.description", true),
					listField("recipients", "config.schema.channels.telegram.recipients.label", "config.schema.channels.telegram.recipients.description", true),
				},
			},
			{
				Key:         "bark",
				Label:       "config.schema.channels.bark.label",
				Description: "config.schema.channels.bark.description",
				Fields: []Field{
					textField("endpoint", "config.schema.channels.bark.endpoint.label", "config.schema.channels.bark.endpoint.description", "config.schema.channels.bark.endpoint.placeholder", false),
					listField("recipients", "config.schema.channels.bark.recipients.label", "config.schema.channels.bark.recipients.description", true),
				},
			},
			{
				Key:         "gotify",
				Label:       "config.schema.channels.gotify.label",
				Description: "config.schema.channels.gotify.description",
				Fields: []Field{
					textField("endpoint", "config.schema.channels.gotify.endpoint.label", "config.schema.channels.gotify.endpoint.description", "config.schema.channels.gotify.endpoint.placeholder", true),
					listField("recipients", "config.schema.channels.gotify.recipients.label", "config.schema.channels.gotify.recipients.description", true),
					numberField("priority", "config.schema.channels.gotify.priority.label", "config.schema.channels.gotify.priority.description", 0, 10, false),
				},
			},
			{
				Key:         "sc3",
				Label:       "config.schema.channels.sc3.label",
				Description: "config.schema.channels.sc3.description",
				Fields: []Field{
					textField("endpoint", "config.schema.channels.sc3.endpoint.label", "config.schema.channels.sc3.endpoint.description", "config.schema.channels.sc3.endpoint.placeholder", true),
				},
			},
			{
				Key:         "http",
				Label:       "config.schema.channels.http.label",
				Description: "config.schema.channels.http.description",
				Fields: []Field{
					textField("endpoint", "config.schema.channels.http.endpoint.label", "config.schema.channels.http.endpoint.description", "config.schema.channels.http.endpoint.placeholder", true),
					{
						Key:         "headers",
						Label:       "config.schema.channels.http.headers.label",
						Description: "config.schema.channels.http.headers.description",
						Control:     controlKeyValue,
					},
				},
			},
			{
				Key:         "email",
				Label:       "config.schema.channels.email.label",
				Description: "config.schema.channels.email.description",
				Fields: []Field{
					textField("smtpHost", "config.schema.channels.email.smtpHost.label", "config.schema.channels.email.smtpHost.description", "config.schema.channels.email.smtpHost.placeholder", true),
					numberField("smtpPort", "config.schema.channels.email.smtpPort.label", "config.schema.channels.email.smtpPort.description", 1, 65535, true),
					textField("smtpUsername", "config.schema.channels.email.smtpUsername.label", "config.schema.channels.email.smtpUsername.description", "config.schema.channels.email.smtpUsername.placeholder", false),
					passwordField("smtpPassword", "config.schema.channels.email.smtpPassword.label", "config.schema.channels.email.smtpPassword.description", false),
					textField("from", "config.schema.channels.email.from.label", "config.schema.channels.email.from.description", "config.schema.channels.email.from.placeholder", true),
					listField("recipients", "config.schema.channels.email.recipients.label", "config.schema.channels.email.recipients.description", true),
					selectField("tlsPolicy", "config.schema.channels.email.tlsPolicy.label", "config.schema.channels.email.tlsPolicy.description", false, []Option{
						{Label: "config.schema.channels.email.tlsPolicy.options.mandatory", Value: "mandatory"},
						{Label: "config.schema.channels.email.tlsPolicy.options.opportunistic", Value: "opportunistic"},
						{Label: "config.schema.channels.email.tlsPolicy.options.none", Value: "none"},
					}),
					{
						Key:         "ssl",
						Label:       "config.schema.channels.email.ssl.label",
						Description: "config.schema.channels.email.ssl.description",
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
