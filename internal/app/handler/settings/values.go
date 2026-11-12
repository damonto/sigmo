package settings

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/notify"
	appsettings "github.com/damonto/sigmo/internal/pkg/settings"
)

func applyAuthSettings(current *appsettings.Settings, auth appsettings.Auth) error {
	current.Auth = auth
	return validateSettings(*current)
}

func applyNotificationChannelSettings(current *appsettings.Settings, name string, channel appsettings.Channel) error {
	current.Channels[name] = channel
	// Saved channels become selectable in both clients even before either use is enabled.
	if _, err := notify.ForChannels(current, []string{name}); err != nil {
		return err
	}
	return validateSettings(*current)
}

func responseFromSettings(current appsettings.Settings) Response {
	return Response{
		Schema: settingsSchema(),
		Values: valuesFromSettings(current),
	}
}

func valuesFromSettings(current appsettings.Settings) Values {
	return Values{
		Auth:          authValuesFromSettings(current.Auth),
		Notifications: current.Notifications,
		Proxy:         proxyValuesFromSettings(current.ProxySettings()),
		Channels:      channelValuesFromSettings(current.Channels),
	}
}

func normalizeAuthValues(auth UpdateAuthRequest) UpdateAuthRequest {
	auth.Mode = strings.TrimSpace(auth.Mode)
	auth.OTPChannels = normalizeNames(trimNames(auth.OTPChannels))
	// The issuer is an exact identifier; a trailing slash can be significant.
	auth.OIDCIssuer = strings.TrimSpace(auth.OIDCIssuer)
	auth.OIDCClientID = strings.TrimSpace(auth.OIDCClientID)
	auth.OIDCAllowedSubjects = normalizeNames(trimStringSlice(auth.OIDCAllowedSubjects))
	auth.AccessTeamDomain = strings.TrimRight(strings.TrimSpace(auth.AccessTeamDomain), "/")
	auth.AccessAudience = strings.TrimSpace(auth.AccessAudience)
	auth.AccessAllowedSubjects = normalizeNames(trimStringSlice(auth.AccessAllowedSubjects))
	return auth
}

func authSettingsFromRequest(current appsettings.Auth, req UpdateAuthRequest) appsettings.Auth {
	secret := req.OIDCClientSecret
	if secret == "" && current.OIDC.Issuer == req.OIDCIssuer && current.OIDC.ClientID == req.OIDCClientID {
		secret = current.OIDC.ClientSecret
	}
	return appsettings.Auth{
		Mode: req.Mode, SessionMaxDays: req.SessionMaxDays,
		OTP:              appsettings.OTP{Channels: req.OTPChannels},
		OIDC:             appsettings.OIDC{Issuer: req.OIDCIssuer, ClientID: req.OIDCClientID, ClientSecret: secret, AllowedSubjects: req.OIDCAllowedSubjects},
		CloudflareAccess: appsettings.CloudflareAccess{TeamDomain: req.AccessTeamDomain, Audience: req.AccessAudience, AllowedSubjects: req.AccessAllowedSubjects},
	}
}

func authValuesFromSettings(auth appsettings.Auth) AuthValues {
	return AuthValues{Mode: auth.Mode, SessionMaxDays: auth.SessionMaxDays,
		OTPChannels: slices.Clone(auth.OTP.Channels), OIDCIssuer: auth.OIDC.Issuer, OIDCClientID: auth.OIDC.ClientID, OIDCAllowedSubjects: slices.Clone(auth.OIDC.AllowedSubjects),
		AccessTeamDomain: auth.CloudflareAccess.TeamDomain, AccessAudience: auth.CloudflareAccess.Audience, AccessAllowedSubjects: slices.Clone(auth.CloudflareAccess.AllowedSubjects)}
}

func normalizeProxyValues(proxy ProxyValues) ProxyValues {
	proxy.ListenAddress = strings.TrimSpace(proxy.ListenAddress)
	return proxy
}

func proxySettingsFromValues(proxy ProxyValues) *appsettings.Proxy {
	return &appsettings.Proxy{
		ListenAddress: proxy.ListenAddress,
		HTTPPort:      proxy.HTTPPort,
		SOCKS5Port:    proxy.SOCKS5Port,
		Password:      proxy.Password,
	}
}

func proxyValuesFromSettings(proxy appsettings.Proxy) ProxyValues {
	return ProxyValues{
		ListenAddress: proxy.ListenAddress,
		HTTPPort:      proxy.HTTPPort,
		SOCKS5Port:    proxy.SOCKS5Port,
		Password:      proxy.Password,
	}
}

func normalizeChannelValue(channel ChannelValues) ChannelValues {
	channel.Endpoint = strings.TrimSpace(channel.Endpoint)
	channel.BotToken = strings.TrimSpace(channel.BotToken)
	channel.Recipients = trimStringSlice(channel.Recipients)
	channel.Headers = trimHeaders(channel.Headers)
	channel.SMTPHost = strings.TrimSpace(channel.SMTPHost)
	channel.SMTPUsername = strings.TrimSpace(channel.SMTPUsername)
	channel.SMTPPassword = strings.TrimSpace(channel.SMTPPassword)
	channel.From = strings.TrimSpace(channel.From)
	channel.TLSPolicy = strings.ToLower(strings.TrimSpace(channel.TLSPolicy))
	return channel
}

func filterChannelValue(name string, channel ChannelValues) ChannelValues {
	values := ChannelValues{}
	switch name {
	case "telegram":
		values.Endpoint = channel.Endpoint
		values.BotToken = channel.BotToken
		values.Recipients = channel.Recipients
	case "bark":
		values.Endpoint = channel.Endpoint
		values.Recipients = channel.Recipients
	case "gotify":
		values.Endpoint = channel.Endpoint
		values.Recipients = channel.Recipients
		values.Priority = channel.Priority
	case "sc3":
		values.Endpoint = channel.Endpoint
	case "lark":
		values.Endpoint = channel.Endpoint
	case "wecom":
		values.Endpoint = channel.Endpoint
	case "http":
		values.Endpoint = channel.Endpoint
		values.Headers = channel.Headers
	case "email":
		values.SMTPHost = channel.SMTPHost
		values.SMTPPort = channel.SMTPPort
		values.SMTPUsername = channel.SMTPUsername
		values.SMTPPassword = channel.SMTPPassword
		values.From = channel.From
		values.Recipients = channel.Recipients
		values.TLSPolicy = channel.TLSPolicy
		values.SSL = channel.SSL
	}
	return values
}

func channelSettingsFromValues(name string, channel ChannelValues) appsettings.Channel {
	normalized := appsettings.Channel{}
	switch name {
	case "telegram":
		normalized.Endpoint = channel.Endpoint
		normalized.BotToken = channel.BotToken
		normalized.Recipients = normalizeRecipients(channel.Recipients)
	case "bark":
		normalized.Endpoint = channel.Endpoint
		normalized.Recipients = normalizeRecipients(channel.Recipients)
	case "gotify":
		normalized.Endpoint = channel.Endpoint
		normalized.Recipients = normalizeRecipients(channel.Recipients)
		normalized.Priority = channel.Priority
	case "sc3":
		normalized.Endpoint = channel.Endpoint
	case "lark":
		normalized.Endpoint = channel.Endpoint
	case "wecom":
		normalized.Endpoint = channel.Endpoint
	case "http":
		normalized.Endpoint = channel.Endpoint
		normalized.Headers = normalizeHeaders(channel.Headers)
	case "email":
		normalized.SMTPHost = channel.SMTPHost
		normalized.SMTPPort = channel.SMTPPort
		normalized.SMTPUsername = channel.SMTPUsername
		normalized.SMTPPassword = channel.SMTPPassword
		normalized.From = channel.From
		normalized.Recipients = normalizeRecipients(channel.Recipients)
		normalized.TLSPolicy = channel.TLSPolicy
		normalized.SSL = channel.SSL
	}
	return normalized
}

func channelValuesFromSettings(channels map[string]appsettings.Channel) map[string]ChannelValues {
	values := make(map[string]ChannelValues, len(channels))
	for name, channel := range channels {
		values[name] = channelSettingsValues(name, channel)
	}
	return values
}

func channelSettingsValues(name string, channel appsettings.Channel) ChannelValues {
	values := ChannelValues{}
	switch name {
	case "telegram":
		values.Endpoint = channel.Endpoint
		values.BotToken = channel.BotToken
		values.Recipients = channel.Recipients.Strings()
	case "bark":
		values.Endpoint = channel.Endpoint
		values.Recipients = channel.Recipients.Strings()
	case "gotify":
		values.Endpoint = channel.Endpoint
		values.Recipients = channel.Recipients.Strings()
		values.Priority = channel.Priority
	case "sc3":
		values.Endpoint = channel.Endpoint
	case "lark":
		values.Endpoint = channel.Endpoint
	case "wecom":
		values.Endpoint = channel.Endpoint
	case "http":
		values.Endpoint = channel.Endpoint
		values.Headers = cloneHeaders(channel.Headers)
	case "email":
		values.SMTPHost = channel.SMTPHost
		values.SMTPPort = channel.SMTPPort
		values.SMTPUsername = channel.SMTPUsername
		values.SMTPPassword = channel.SMTPPassword
		values.From = channel.From
		values.Recipients = channel.Recipients.Strings()
		values.TLSPolicy = channel.TLSPolicy
		values.SSL = channel.SSL
	}
	return values
}

func normalizeNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	var normalized []string
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	slices.Sort(normalized)
	return normalized
}

func trimNames(names []string) []string {
	values := trimStringSlice(names)
	for i, value := range values {
		values[i] = strings.ToLower(value)
	}
	return values
}

func trimStringSlice(values []string) []string {
	trimmed := slices.Clone(values)
	for i := range trimmed {
		trimmed[i] = strings.TrimSpace(trimmed[i])
	}
	return trimmed
}

func normalizeRecipients(recipients []string) appsettings.Recipients {
	normalized := make(appsettings.Recipients, 0, len(recipients))
	for _, recipient := range recipients {
		normalized = append(normalized, appsettings.Recipient(recipient))
	}
	return normalized
}

func trimHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	trimmed := make(map[string]string, len(headers))
	for key, value := range headers {
		trimmed[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return trimmed
}

func normalizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	return maps.Clone(headers)
}

func validateSettings(current appsettings.Settings) error {
	return validateSettingsWithOptions(current, appsettings.ValidationOptions{})
}

func validateSettingsWithOptions(current appsettings.Settings, options appsettings.ValidationOptions) error {
	allowedChannels := allowedChannelNames()
	for name := range current.Channels {
		if _, ok := allowedChannels[name]; !ok {
			return fmt.Errorf("unsupported channel %q", name)
		}
	}
	if err := current.ValidateWithOptions(options); err != nil {
		return err
	}
	targets := append(slices.Clone(current.Notifications.Channels), current.Auth.OTP.Channels...)
	_, err := notify.ForChannels(&current, targets)
	return err
}

func allowedChannelNames() map[string]struct{} {
	schema := settingsSchema()
	names := make(map[string]struct{}, len(schema.Channels))
	for _, channel := range schema.Channels {
		names[channel.Key] = struct{}{}
	}
	return names
}

func internetProxyConfig(current appsettings.Settings) internet.ProxyConfig {
	proxy := current.ProxySettings()
	return internet.ProxyConfig{
		ListenAddress: proxy.ListenAddress,
		HTTPPort:      proxy.HTTPPort,
		SOCKS5Port:    proxy.SOCKS5Port,
		Password:      proxy.Password,
	}
}

func cloneHeaders(headers map[string]string) map[string]string {
	return maps.Clone(headers)
}
