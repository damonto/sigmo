package config

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/forwarder"
	"github.com/damonto/sigmo/internal/app/httpapi"
	pkgconfig "github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/notify"
)

type Handler struct {
	store             *pkgconfig.Store
	internetConnector *internet.Connector
	relay             *forwarder.Relay
}

const (
	errorCodeUpdateConfigInvalidRequest = "update_config_invalid_request"
	errorCodeUpdateConfigInvalid        = "update_config_invalid"
	errorCodeUpdateConfigFailed         = "update_config_failed"
	errorCodeReloadProxyConfigFailed    = "reload_proxy_config_failed"
	errorCodeReloadRelayFailed          = "reload_notification_relay_failed"
)

var (
	errAuthProvidersRequired = errors.New("auth providers are required when otp is enabled")
)

func New(store *pkgconfig.Store, internetConnector *internet.Connector, relay *forwarder.Relay) *Handler {
	return &Handler{
		store:             store,
		internetConnector: internetConnector,
		relay:             relay,
	}
}

func (h *Handler) Get(c *echo.Context) error {
	cfg := h.store.Snapshot()
	return c.JSON(http.StatusOK, responseFromConfig(cfg, nil))
}

func (h *Handler) Update(c *echo.Context) error {
	var req UpdateRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeUpdateConfigInvalidRequest, err)
	}
	req = normalizeRequest(req)
	if err := c.Validate(&req); err != nil {
		return httpapi.UnprocessableEntity(c, errorCodeUpdateConfigInvalid, err)
	}

	current := h.store.Snapshot()
	next := current.Clone()
	next.App = appConfig(req.App)
	next.Channels = normalizeChannels(req.Channels)
	next.Proxy = proxyConfigFromValues(req.Proxy)

	if err := validateConfig(next); err != nil {
		return httpapi.UnprocessableEntity(c, errorCodeUpdateConfigInvalid, err)
	}
	if _, err := notify.New(&next); err != nil {
		return httpapi.UnprocessableEntity(c, errorCodeUpdateConfigInvalid, err)
	}

	restartRequiredFields := restartRequiredFields(current, next)
	saved, err := h.store.Update(func(cfg *pkgconfig.Config) error {
		cfg.App = next.App
		cfg.Proxy = next.Proxy
		cfg.Channels = next.Channels
		return nil
	})
	if err != nil {
		return httpapi.Internal(c, errorCodeUpdateConfigFailed, fmt.Errorf("save config: %w", err))
	}

	applyLogLevel(saved)
	httpapi.SetExposeInternalErrors(!saved.IsProduction())
	if err := h.internetConnector.UpdateProxyConfig(proxyConfig(saved)); err != nil {
		return httpapi.Internal(c, errorCodeReloadProxyConfigFailed, fmt.Errorf("saved config, reload proxy config: %w", err))
	}
	if err := h.relay.Reload(); err != nil {
		return httpapi.Internal(c, errorCodeReloadRelayFailed, fmt.Errorf("saved config, reload notification relay: %w", err))
	}

	return c.JSON(http.StatusOK, responseFromConfig(saved, restartRequiredFields))
}

func responseFromConfig(cfg pkgconfig.Config, restartRequiredFields []string) Response {
	return Response{
		Path:                  cfg.Path,
		Schema:                configSchema(),
		Values:                valuesFromConfig(cfg),
		RestartRequiredFields: restartRequiredFields,
	}
}

func valuesFromConfig(cfg pkgconfig.Config) Values {
	return Values{
		App:      appValuesFromConfig(cfg.App),
		Proxy:    proxyValuesFromConfig(cfg.ProxySettings()),
		Channels: channelValuesFromConfig(cfg.Channels),
	}
}

func normalizeRequest(req UpdateRequest) UpdateRequest {
	req.App = normalizeAppValues(req.App)
	req.Proxy = normalizeProxyValues(req.Proxy)
	req.Channels = filterChannelValues(normalizeChannelValues(req.Channels))
	return req
}

func normalizeAppValues(app AppValues) AppValues {
	app.Environment = strings.ToLower(strings.TrimSpace(app.Environment))
	app.ListenAddress = strings.TrimSpace(app.ListenAddress)
	app.AuthProviders = trimNames(app.AuthProviders)
	return app
}

func appConfig(app AppValues) pkgconfig.App {
	return pkgconfig.App{
		Environment:   app.Environment,
		ListenAddress: app.ListenAddress,
		AuthProviders: normalizeNames(app.AuthProviders),
		OTPRequired:   app.OTPRequired,
	}
}

func appValuesFromConfig(app pkgconfig.App) AppValues {
	return AppValues{
		Environment:   app.Environment,
		ListenAddress: app.ListenAddress,
		AuthProviders: slices.Clone(app.AuthProviders),
		OTPRequired:   app.OTPRequired,
	}
}

func normalizeProxyValues(proxy ProxyValues) ProxyValues {
	proxy.ListenAddress = strings.TrimSpace(proxy.ListenAddress)
	return proxy
}

func proxyConfigFromValues(proxy ProxyValues) *pkgconfig.Proxy {
	return &pkgconfig.Proxy{
		ListenAddress: proxy.ListenAddress,
		HTTPPort:      proxy.HTTPPort,
		SOCKS5Port:    proxy.SOCKS5Port,
		Password:      proxy.Password,
	}
}

func proxyValuesFromConfig(proxy pkgconfig.Proxy) ProxyValues {
	return ProxyValues{
		ListenAddress: proxy.ListenAddress,
		HTTPPort:      proxy.HTTPPort,
		SOCKS5Port:    proxy.SOCKS5Port,
		Password:      proxy.Password,
	}
}

func normalizeChannels(channels map[string]ChannelValues) map[string]pkgconfig.Channel {
	normalized := make(map[string]pkgconfig.Channel, len(channels))
	for name, channel := range channels {
		normalized[name] = channelConfig(name, channel)
	}
	return normalized
}

func normalizeChannelValues(channels map[string]ChannelValues) map[string]ChannelValues {
	normalized := make(map[string]ChannelValues, len(channels))
	for name, channel := range channels {
		name = strings.ToLower(strings.TrimSpace(name))
		normalized[name] = normalizeChannelValue(channel)
	}
	return normalized
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

func filterChannelValues(channels map[string]ChannelValues) map[string]ChannelValues {
	filtered := make(map[string]ChannelValues, len(channels))
	for name, channel := range channels {
		filtered[name] = filterChannelValue(name, channel)
	}
	return filtered
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

func channelConfig(name string, channel ChannelValues) pkgconfig.Channel {
	normalized := pkgconfig.Channel{
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

func channelValuesFromConfig(channels map[string]pkgconfig.Channel) map[string]ChannelValues {
	values := make(map[string]ChannelValues, len(channels))
	for name, channel := range channels {
		values[name] = channelValues(name, channel)
	}
	return values
}

func channelValues(name string, channel pkgconfig.Channel) ChannelValues {
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

func normalizeRecipients(recipients []string) pkgconfig.Recipients {
	normalized := make(pkgconfig.Recipients, 0, len(recipients))
	for _, recipient := range recipients {
		normalized = append(normalized, pkgconfig.Recipient(recipient))
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

func validateConfig(cfg pkgconfig.Config) error {
	allowedChannels := allowedChannelNames()
	for name := range cfg.Channels {
		if _, ok := allowedChannels[name]; !ok {
			return fmt.Errorf("unsupported channel %q", name)
		}
	}
	if cfg.App.OTPRequired && len(cfg.App.AuthProviders) == 0 {
		return errAuthProvidersRequired
	}
	for _, provider := range cfg.App.AuthProviders {
		channel, ok := cfg.Channels[provider]
		if !ok || !channel.IsEnabled() {
			return fmt.Errorf("auth provider %q must be an enabled channel", provider)
		}
	}
	return nil
}

func allowedChannelNames() map[string]struct{} {
	schema := configSchema()
	names := make(map[string]struct{}, len(schema.Channels))
	for _, channel := range schema.Channels {
		names[channel.Key] = struct{}{}
	}
	return names
}

func restartRequiredFields(oldConfig pkgconfig.Config, newConfig pkgconfig.Config) []string {
	var fields []string
	if oldConfig.App.ListenAddress != newConfig.App.ListenAddress {
		fields = append(fields, "app.listenAddress")
	}
	return fields
}

func applyLogLevel(cfg pkgconfig.Config) {
	if cfg.IsProduction() {
		slog.SetLogLoggerLevel(slog.LevelInfo)
		return
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)
}

func proxyConfig(cfg pkgconfig.Config) internet.ProxyConfig {
	proxy := cfg.ProxySettings()
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
