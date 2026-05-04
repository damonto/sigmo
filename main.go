package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/damonto/sigmo/internal/app/forwarder"
	"github.com/damonto/sigmo/internal/app/router"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/validator"
)

var (
	BuildVersion string
	configPath   string
)

func init() {
	flag.StringVar(&configPath, "config", "config.toml", "path to config file")
}

func main() {
	flag.Parse()
	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("unable to load config", "error", err)
		os.Exit(1)
	}
	if !cfg.IsProduction() {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	slog.Info("server starting", "version", BuildVersion)

	manager, err := modem.NewManager()
	if err != nil {
		slog.Error("unable to connect modem manager", "error", err)
		os.Exit(1)
	}

	server := echo.New()
	server.Logger = slog.Default()
	server.Validator = validator.New()
	if !cfg.IsProduction() {
		server.Use(middleware.RequestLogger())
	}
	server.Use(middleware.RequestID())
	server.Use(middleware.Recover())
	server.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions},
		AllowHeaders: []string{"*"},
	}))
	internetConnector := newInternetConnector(cfg)
	if err := recoverInternetConnections(manager, internetConnector); err != nil {
		slog.Error("recover internet connections", "error", err)
	}
	router.Register(server, cfg, manager, internetConnector)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go internetConnector.RunAlwaysOn(ctx, manager)

	relay, err := forwarder.New(cfg, manager)
	if err != nil {
		slog.Error("unable to configure message relay", "error", err)
		os.Exit(1)
	}

	if relay.Enabled() {
		go func() {
			if err := relay.Run(ctx); err != nil {
				slog.Error("message relay stopped", "error", err)
				stop()
			}
		}()
	}

	startConfig := echo.StartConfig{
		Address:         cfg.App.ListenAddress,
		HideBanner:      true,
		GracefulTimeout: 5 * time.Second,
	}
	if err := startConfig.Start(ctx, server); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("http server stopped", "error", err)
		os.Exit(1)
	}
}

func newInternetConnector(cfg *config.Config) *internet.Connector {
	proxyConfig := cfg.ProxySettings()
	proxy := internet.NewProxy(internet.ProxyConfig{
		ListenAddress: proxyConfig.ListenAddress,
		HTTPPort:      proxyConfig.HTTPPort,
		SOCKS5Port:    proxyConfig.SOCKS5Port,
		Password:      proxyConfig.Password,
	})
	return internet.NewConnectorWithProxy(proxy)
}

func recoverInternetConnections(manager *modem.Manager, connector *internet.Connector) error {
	modemMap, err := manager.Modems()
	if err != nil {
		return fmt.Errorf("list modems: %w", err)
	}
	modems := make([]*modem.Modem, 0, len(modemMap))
	for _, modem := range modemMap {
		modems = append(modems, modem)
	}
	return connector.Recover(modems)
}
