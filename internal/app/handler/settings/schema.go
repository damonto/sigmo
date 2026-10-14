package settings

import appsettings "github.com/damonto/sigmo/internal/pkg/settings"

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

func settingsSchema() Schema {
	return Schema{
		Auth: []Field{
			{Key: "mode", Label: "settings.schema.auth.mode", Control: controlSelect, Options: []Option{{Label: "settings.authNone", Value: "none"}, {Label: "settings.authOTP", Value: "otp"}, {Label: "settings.authOIDC", Value: "oidc"}, {Label: "settings.authAccess", Value: "cloudflare_access"}}},
			numberField("sessionMaxDays", "settings.schema.auth.sessionMaxDays", "settings.schema.auth.sessionMaxDaysDescription", 1, appsettings.MaxSessionMaxDays, true),
			{Key: "otpChannels", Label: "settings.schema.auth.otpChannels", Control: controlChannelList},
			textField("oidcIssuer", "settings.schema.auth.oidcIssuer", "", "", false),
			textField("oidcClientId", "settings.schema.auth.oidcClientId", "", "", false),
			passwordField("oidcClientSecret", "settings.schema.auth.oidcClientSecret", "settings.schema.auth.keepSecret", false),
			listField("oidcAllowedSubjects", "settings.schema.auth.oidcAllowedSubjects", "", false),
			textField("accessTeamDomain", "settings.schema.auth.accessTeamDomain", "", "", false),
			textField("accessAudience", "settings.schema.auth.accessAudience", "", "", false),
			listField("accessAllowedSubjects", "settings.schema.auth.accessAllowedSubjects", "", false),
		},
		Proxy: []Field{
			textField("listenAddress", "settings.schema.proxy.listenAddress.label", "settings.schema.proxy.listenAddress.description", "settings.schema.proxy.listenAddress.placeholder", true),
			numberField("httpPort", "settings.schema.proxy.httpPort.label", "settings.schema.proxy.httpPort.description", 1, 65535, true),
			numberField("socks5Port", "settings.schema.proxy.socks5Port.label", "settings.schema.proxy.socks5Port.description", 1, 65535, true),
			passwordField("password", "settings.schema.proxy.password.label", "settings.schema.proxy.password.description", false),
		},
		Channels: []ChannelSchema{
			{
				Key:         "telegram",
				Label:       "settings.schema.channels.telegram.label",
				Description: "settings.schema.channels.telegram.description",
				Fields: []Field{
					textField("endpoint", "settings.schema.channels.telegram.endpoint.label", "settings.schema.channels.telegram.endpoint.description", "settings.schema.channels.telegram.endpoint.placeholder", false),
					passwordField("botToken", "settings.schema.channels.telegram.botToken.label", "settings.schema.channels.telegram.botToken.description", true),
					listField("recipients", "settings.schema.channels.telegram.recipients.label", "settings.schema.channels.telegram.recipients.description", true),
				},
			},
			{
				Key:         "bark",
				Label:       "settings.schema.channels.bark.label",
				Description: "settings.schema.channels.bark.description",
				Fields: []Field{
					textField("endpoint", "settings.schema.channels.bark.endpoint.label", "settings.schema.channels.bark.endpoint.description", "settings.schema.channels.bark.endpoint.placeholder", false),
					listField("recipients", "settings.schema.channels.bark.recipients.label", "settings.schema.channels.bark.recipients.description", true),
				},
			},
			{
				Key:         "gotify",
				Label:       "settings.schema.channels.gotify.label",
				Description: "settings.schema.channels.gotify.description",
				Fields: []Field{
					textField("endpoint", "settings.schema.channels.gotify.endpoint.label", "settings.schema.channels.gotify.endpoint.description", "settings.schema.channels.gotify.endpoint.placeholder", true),
					listField("recipients", "settings.schema.channels.gotify.recipients.label", "settings.schema.channels.gotify.recipients.description", true),
					numberField("priority", "settings.schema.channels.gotify.priority.label", "settings.schema.channels.gotify.priority.description", 0, 10, false),
				},
			},
			{
				Key:         "sc3",
				Label:       "settings.schema.channels.sc3.label",
				Description: "settings.schema.channels.sc3.description",
				Fields: []Field{
					textField("endpoint", "settings.schema.channels.sc3.endpoint.label", "settings.schema.channels.sc3.endpoint.description", "settings.schema.channels.sc3.endpoint.placeholder", true),
				},
			},
			{
				Key:         "lark",
				Label:       "settings.schema.channels.lark.label",
				Description: "settings.schema.channels.lark.description",
				Fields: []Field{
					textField("endpoint", "settings.schema.channels.lark.endpoint.label", "settings.schema.channels.lark.endpoint.description", "settings.schema.channels.lark.endpoint.placeholder", true),
				},
			},
			{
				Key:         "wecom",
				Label:       "settings.schema.channels.wecom.label",
				Description: "settings.schema.channels.wecom.description",
				Fields: []Field{
					textField("endpoint", "settings.schema.channels.wecom.endpoint.label", "settings.schema.channels.wecom.endpoint.description", "settings.schema.channels.wecom.endpoint.placeholder", true),
				},
			},
			{
				Key:         "http",
				Label:       "settings.schema.channels.http.label",
				Description: "settings.schema.channels.http.description",
				Fields: []Field{
					textField("endpoint", "settings.schema.channels.http.endpoint.label", "settings.schema.channels.http.endpoint.description", "settings.schema.channels.http.endpoint.placeholder", true),
					{
						Key:         "headers",
						Label:       "settings.schema.channels.http.headers.label",
						Description: "settings.schema.channels.http.headers.description",
						Control:     controlKeyValue,
					},
				},
			},
			{
				Key:         "email",
				Label:       "settings.schema.channels.email.label",
				Description: "settings.schema.channels.email.description",
				Fields: []Field{
					textField("smtpHost", "settings.schema.channels.email.smtpHost.label", "settings.schema.channels.email.smtpHost.description", "settings.schema.channels.email.smtpHost.placeholder", true),
					numberField("smtpPort", "settings.schema.channels.email.smtpPort.label", "settings.schema.channels.email.smtpPort.description", 1, 65535, true),
					textField("smtpUsername", "settings.schema.channels.email.smtpUsername.label", "settings.schema.channels.email.smtpUsername.description", "settings.schema.channels.email.smtpUsername.placeholder", false),
					passwordField("smtpPassword", "settings.schema.channels.email.smtpPassword.label", "settings.schema.channels.email.smtpPassword.description", false),
					textField("from", "settings.schema.channels.email.from.label", "settings.schema.channels.email.from.description", "settings.schema.channels.email.from.placeholder", true),
					listField("recipients", "settings.schema.channels.email.recipients.label", "settings.schema.channels.email.recipients.description", true),
					selectField("tlsPolicy", "settings.schema.channels.email.tlsPolicy.label", "settings.schema.channels.email.tlsPolicy.description", false, []Option{
						{Label: "settings.schema.channels.email.tlsPolicy.options.mandatory", Value: "mandatory"},
						{Label: "settings.schema.channels.email.tlsPolicy.options.opportunistic", Value: "opportunistic"},
						{Label: "settings.schema.channels.email.tlsPolicy.options.none", Value: "none"},
					}),
					{
						Key:         "ssl",
						Label:       "settings.schema.channels.email.ssl.label",
						Description: "settings.schema.channels.email.ssl.description",
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
