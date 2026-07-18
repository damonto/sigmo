package settings

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/forwarder"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/notify"
	appsettings "github.com/damonto/sigmo/internal/pkg/settings"
)

type Handler struct {
	store             *appsettings.Store
	internetConnector *internet.Connector
	relay             *forwarder.Relay
}

const (
	errorCodeUpdateAuthInvalidRequest         = "update_auth_invalid_request"
	errorCodeUpdateAuthInvalid                = "update_auth_invalid"
	errorCodeUpdateAuthFailed                 = "update_auth_failed"
	errorCodeUpdateProxyInvalidRequest        = "update_proxy_invalid_request"
	errorCodeUpdateProxyInvalid               = "update_proxy_invalid"
	errorCodeUpdateProxyFailed                = "update_proxy_failed"
	errorCodeUpdateNotificationInvalidRequest = "update_notification_invalid_request"
	errorCodeUpdateNotificationInvalid        = "update_notification_invalid"
	errorCodeUpdateNotificationFailed         = "update_notification_failed"
	errorCodeReloadProxySettingsFailed        = "reload_proxy_settings_failed"
	errorCodeReloadRelayFailed                = "reload_notification_relay_failed"
)

var (
	errAuthProvidersRequired = errors.New("auth providers are required when otp is enabled")
)

func New(store *appsettings.Store, internetConnector *internet.Connector, relay *forwarder.Relay) *Handler {
	return &Handler{
		store:             store,
		internetConnector: internetConnector,
		relay:             relay,
	}
}

func (h *Handler) Get(c *echo.Context) error {
	current := h.store.Snapshot()
	return c.JSON(http.StatusOK, responseFromSettings(current))
}

func (h *Handler) UpdateAuth(c *echo.Context) error {
	var req AuthValues
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeUpdateAuthInvalidRequest, err)
	}
	req = normalizeAuthValues(req)
	if err := c.Validate(&req); err != nil {
		return httpapi.UnprocessableEntity(c, errorCodeUpdateAuthInvalid, err)
	}

	auth := authSettingsFromValues(req)
	var invalidErr error
	saved, err := h.store.Update(c.Request().Context(), func(current *appsettings.Settings) error {
		invalidErr = applyAuthSettings(current, auth)
		return invalidErr
	})
	if invalidErr != nil {
		return httpapi.UnprocessableEntity(c, errorCodeUpdateAuthInvalid, invalidErr)
	}
	if err != nil {
		return httpapi.Internal(c, errorCodeUpdateAuthFailed, fmt.Errorf("save auth settings: %w", err))
	}

	return c.JSON(http.StatusOK, responseFromSettings(saved))
}

func (h *Handler) UpdateProxy(c *echo.Context) error {
	var req ProxyValues
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeUpdateProxyInvalidRequest, err)
	}
	req = normalizeProxyValues(req)
	if err := c.Validate(&req); err != nil {
		return httpapi.UnprocessableEntity(c, errorCodeUpdateProxyInvalid, err)
	}

	proxy := proxySettingsFromValues(req)
	saved, err := h.store.Update(c.Request().Context(), func(current *appsettings.Settings) error {
		current.Proxy = proxy
		return nil
	})
	if err != nil {
		return httpapi.Internal(c, errorCodeUpdateProxyFailed, fmt.Errorf("save proxy settings: %w", err))
	}

	if err := h.internetConnector.UpdateProxyConfig(internetProxyConfig(saved)); err != nil {
		return httpapi.Internal(c, errorCodeReloadProxySettingsFailed, fmt.Errorf("saved proxy settings, reload proxy: %w", err))
	}

	return c.JSON(http.StatusOK, responseFromSettings(saved))
}

func (h *Handler) UpdateNotificationChannel(c *echo.Context) error {
	name := strings.ToLower(strings.TrimSpace(c.Param("channel")))
	if _, ok := allowedChannelNames()[name]; !ok {
		return httpapi.UnprocessableEntity(
			c,
			errorCodeUpdateNotificationInvalid,
			fmt.Errorf("notification channel %q is not supported", name),
		)
	}

	var req ChannelValues
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeUpdateNotificationInvalidRequest, err)
	}
	req = filterChannelValue(name, normalizeChannelValue(req))
	if err := c.Validate(&req); err != nil {
		return httpapi.UnprocessableEntity(c, errorCodeUpdateNotificationInvalid, err)
	}

	channel := channelSettingsFromValues(name, req)
	var invalidErr error
	saved, err := h.store.Update(c.Request().Context(), func(current *appsettings.Settings) error {
		invalidErr = applyNotificationChannelSettings(current, name, channel)
		return invalidErr
	})
	if invalidErr != nil {
		return httpapi.UnprocessableEntity(c, errorCodeUpdateNotificationInvalid, invalidErr)
	}
	if err != nil {
		return httpapi.Internal(c, errorCodeUpdateNotificationFailed, fmt.Errorf("save notification channel: %w", err))
	}

	if err := h.relay.Reload(); err != nil {
		return httpapi.Internal(c, errorCodeReloadRelayFailed, fmt.Errorf("saved notification channel, reload notification relay: %w", err))
	}

	return c.JSON(http.StatusOK, responseFromSettings(saved))
}

func applyAuthSettings(current *appsettings.Settings, auth appsettings.Auth) error {
	current.Auth = auth
	return validateSettings(*current)
}

func applyNotificationChannelSettings(current *appsettings.Settings, name string, channel appsettings.Channel) error {
	current.Channels[name] = channel
	if !channel.IsEnabled() {
		current.Auth.AuthProviders = slices.DeleteFunc(
			current.Auth.AuthProviders,
			func(provider string) bool { return provider == name },
		)
	}
	if err := validateSettings(*current); err != nil {
		return err
	}
	_, err := notify.New(current)
	return err
}

func responseFromSettings(current appsettings.Settings) Response {
	return Response{
		Schema: settingsSchema(),
		Values: valuesFromSettings(current),
	}
}

func valuesFromSettings(current appsettings.Settings) Values {
	return Values{
		Auth:     authValuesFromSettings(current.Auth),
		Proxy:    proxyValuesFromSettings(current.ProxySettings()),
		Channels: channelValuesFromSettings(current.Channels),
	}
}

func normalizeAuthValues(auth AuthValues) AuthValues {
	auth.AuthProviders = trimNames(auth.AuthProviders)
	return auth
}

func authSettingsFromValues(auth AuthValues) appsettings.Auth {
	return appsettings.Auth{
		AuthProviders: normalizeNames(auth.AuthProviders),
		OTPRequired:   auth.OTPRequired,
	}
}

func authValuesFromSettings(auth appsettings.Auth) AuthValues {
	return AuthValues{
		AuthProviders: slices.Clone(auth.AuthProviders),
		OTPRequired:   auth.OTPRequired,
	}
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
	values := ChannelValues{
		Enabled: channel.Enabled,
	}
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
	normalized := appsettings.Channel{
		Enabled: channel.Enabled,
	}
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
	enabled := channel.IsEnabled()
	values := ChannelValues{
		Enabled: &enabled,
	}
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
	allowedChannels := allowedChannelNames()
	for name := range current.Channels {
		if _, ok := allowedChannels[name]; !ok {
			return fmt.Errorf("unsupported channel %q", name)
		}
	}
	if current.Auth.OTPRequired && len(current.Auth.AuthProviders) == 0 {
		return errAuthProvidersRequired
	}
	for _, provider := range current.Auth.AuthProviders {
		channel, ok := current.Channels[provider]
		if !ok || !channel.IsEnabled() {
			return fmt.Errorf("auth provider %q must be an enabled channel", provider)
		}
	}
	return nil
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
