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
	"github.com/damonto/sigmo/internal/app/httpapi"
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
	flag.StringVar(&configPath, "config", "", "path to config file")
}

func main() {
	flag.Parse()
	configExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configExplicit = true
		}
	})
	cfg, err := loadConfig(configPath, configExplicit)
	if err != nil {
		slog.Error("unable to load config", "error", err)
		os.Exit(1)
	}
	store := config.NewStore(cfg)
	applyLogLevel(store)
	httpapi.SetExposeInternalErrors(!store.IsProduction())
	slog.Info("server starting", "version", BuildVersion)

	manager, err := modem.NewManager()
	if err != nil {
		slog.Error("unable to connect modem manager", "error", err)
		os.Exit(1)
	}

	server := echo.New()
	server.Logger = slog.Default()
	server.Validator = validator.New()
	requestLogger := middleware.RequestLogger()
	server.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		logged := requestLogger(next)
		return func(c *echo.Context) error {
			if store.IsProduction() {
				return next(c)
			}
			return logged(c)
		}
	})
	server.Use(middleware.RequestID())
	server.Use(middleware.Recover())
	server.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions},
		AllowHeaders: []string{"*"},
	}))
	internetConnector, err := newInternetConnector(store)
	if err != nil {
		slog.Error("configure internet connector", "error", err)
		os.Exit(1)
	}
	if err := recoverInternetConnections(manager, internetConnector); err != nil {
		slog.Error("recover internet connections", "error", err)
	}
	relay, err := forwarder.New(store, manager)
	if err != nil {
		slog.Error("unable to configure message relay", "error", err)
		os.Exit(1)
	}
	router.Register(server, store, manager, internetConnector, relay)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := manager.RunSMSStorageDefaults(ctx, modem.SMSStorageME); err != nil {
			slog.Error("SMS storage defaults stopped", "error", err)
		}
	}()

	go internetConnector.RunAlwaysOn(ctx, manager)

	go func() {
		if err := relay.Run(ctx); err != nil {
			slog.Error("message relay stopped", "error", err)
			stop()
		}
	}()

	startConfig := echo.StartConfig{
		Address:         store.Snapshot().App.ListenAddress,
		HideBanner:      true,
		GracefulTimeout: 5 * time.Second,
	}
	if err := startConfig.Start(ctx, server); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("http server stopped", "error", err)
		os.Exit(1)
	}
}

func loadConfig(path string, explicit bool) (*config.Config, error) {
	if explicit {
		return config.Load(path)
	}
	defaultPath, err := config.DefaultPath()
	if err != nil {
		return nil, err
	}
	return config.LoadOrCreate(defaultPath)
}

func applyLogLevel(store *config.Store) {
	if store.IsProduction() {
		slog.SetLogLoggerLevel(slog.LevelInfo)
		return
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)
}

func newInternetConnector(store *config.Store) (*internet.Connector, error) {
	proxyConfig := store.ProxySettings()
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
