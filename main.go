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
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/damonto/sigmo/internal/app/forwarder"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/app/router"
	pcall "github.com/damonto/sigmo/internal/pkg/call"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/validator"
	"github.com/damonto/sigmo/internal/pkg/websheet"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
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

	dbPath, err := cfg.DatabasePath()
	if err != nil {
		slog.Error("configure storage", "error", err)
		os.Exit(1)
	}
	db, err := storage.Open(context.Background(), dbPath)
	if err != nil {
		slog.Error("configure storage", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Warn("close storage", "error", err)
		}
	}()

	registry, err := modem.NewRegistry()
	if err != nil {
		slog.Error("unable to connect modem registry", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := registry.Close(); err != nil {
			slog.Warn("close modem registry", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

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
	internetConnector, err := newInternetConnector(store, db)
	if err != nil {
		slog.Error("configure internet connector", "error", err)
		os.Exit(1)
	}
	startupCtx, cancelStartup := context.WithTimeout(ctx, 15*time.Second)
	if err := modem.EnableDisabled(startupCtx, registry); err != nil {
		slog.Error("enable disabled modems", "error", err)
	}
	if err := recoverInternetConnections(startupCtx, registry, internetConnector); err != nil {
		slog.Error("recover internet connections", "error", err)
	}
	cancelStartup()
	relay, err := forwarder.New(store, registry, db)
	if err != nil {
		slog.Error("unable to configure message relay", "error", err)
		os.Exit(1)
	}
	websheets := websheet.New(websheet.Config{})
	wifiCalling := wificalling.New(wificalling.Config{
		Store:      db,
		OnIncoming: relay.ForwardWiFiCallingSMS,
		Websheets:  websheets,
	})
	callService := pcall.New(db, wifiCalling)
	networkPreferences := modem.NewNetworkPreferencesWithStore(db)
	router.Register(server, router.RegisterConfig{
		Store:              store,
		Registry:           registry,
		Internet:           internetConnector,
		Relay:              relay,
		NetworkPreferences: networkPreferences,
		Storage:            db,
		WiFiCalling:        wifiCalling,
		Calls:              callService,
		Websheets:          websheets,
	})

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := modem.RunEnableDisabled(ctx, registry); err != nil {
			slog.Error("modem enable runner stopped", "error", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := modem.RunSMSStorageDefaults(ctx, registry, modem.SMSStorageME); err != nil {
			slog.Error("SMS storage defaults stopped", "error", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		internetConnector.RunAlwaysOn(ctx, registry)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := networkPreferences.Run(ctx, registry); err != nil {
			slog.Error("network preferences restore stopped", "error", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := relay.Run(ctx); err != nil {
			slog.Error("message relay stopped", "error", err)
			stop()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := wifiCalling.Run(ctx, registry); err != nil {
			slog.Error("Wi-Fi Calling coordinator stopped", "error", err)
			stop()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := callService.Run(ctx); err != nil {
			slog.Error("call service stopped", "error", err)
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
	wg.Wait()
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

func newInternetConnector(store *config.Store, db *storage.Store) (*internet.Connector, error) {
	proxyConfig := store.ProxySettings()
	proxy := internet.NewProxy(internet.ProxyConfig{
		ListenAddress: proxyConfig.ListenAddress,
		HTTPPort:      proxyConfig.HTTPPort,
		SOCKS5Port:    proxyConfig.SOCKS5Port,
		Password:      proxyConfig.Password,
	})
	return internet.NewConnector(internet.ConnectorConfig{Proxy: proxy, State: db})
}

func recoverInternetConnections(ctx context.Context, registry *modem.Registry, connector *internet.Connector) error {
	modemMap, err := registry.Modems(ctx)
	if err != nil {
		return fmt.Errorf("list modems: %w", err)
	}
	modems := make([]*modem.Modem, 0, len(modemMap))
	for _, modem := range modemMap {
		modems = append(modems, modem)
	}
	return connector.Recover(ctx, modems)
}
