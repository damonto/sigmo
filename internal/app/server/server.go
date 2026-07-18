package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/damonto/sigmo/internal/app/forwarder"
	hnetwork "github.com/damonto/sigmo/internal/app/handler/network"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/app/router"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/validator"
	"github.com/damonto/sigmo/internal/pkg/webpush"
)

type Config struct {
	BuildVersion  string
	ListenAddress string
	DBPath        string
	Debug         bool
	Configure     Extension
}

func Run(cfg Config) error {
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = "0.0.0.0:9527"
	}
	applyLogLevel(cfg.Debug)
	httpapi.SetExposeInternalErrors(cfg.Debug)

	resolvedDBPath, err := resolveDBPath(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("configure storage: %w", err)
	}
	db, err := storage.Open(context.Background(), resolvedDBPath)
	if err != nil {
		return fmt.Errorf("configure storage: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Warn("close storage", "error", err)
		}
	}()

	store, err := settings.NewStore(context.Background(), db)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	webPush, err := webpush.New(context.Background(), db)
	if err != nil {
		return fmt.Errorf("configure web push: %w", err)
	}
	reminderScheduler, err := reminder.New(db, store, webPush)
	if err != nil {
		return fmt.Errorf("configure reminders: %w", err)
	}
	slog.Info("server starting", "version", cfg.BuildVersion, "listen_address", cfg.ListenAddress, "db_path", resolvedDBPath)

	registry, err := modem.NewRegistry()
	if err != nil {
		return fmt.Errorf("connect modem registry: %w", err)
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
			if cfg.Debug {
				return logged(c)
			}
			return next(c)
		}
	})
	server.Use(middleware.RequestID())
	server.Use(middleware.Recover())
	server.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions},
		AllowHeaders: []string{"*"},
	}))
	networkPreferences, err := modem.NewNetworkPreferences(db)
	if err != nil {
		return fmt.Errorf("configure network preferences: %w", err)
	}
	enableDisabledPolicy := modem.SkipEnableDisabledInAirplaneMode(networkPreferences)
	internetConnector, err := newInternetConnector(store, db, networkPreferences)
	if err != nil {
		return fmt.Errorf("configure internet connector: %w", err)
	}
	startupCtx, cancelStartup := context.WithTimeout(ctx, 15*time.Second)
	if err := modem.EnableDisabled(startupCtx, registry, enableDisabledPolicy); err != nil {
		slog.Error("enable disabled modems", "error", err)
	}
	if err := recoverInternetConnections(startupCtx, registry, internetConnector); err != nil {
		slog.Error("recover internet connections", "error", err)
	}
	cancelStartup()
	relay, err := forwarder.New(store, registry, db, webPush)
	if err != nil {
		return fmt.Errorf("configure message relay: %w", err)
	}
	runtime := &Runtime{
		Store:              store,
		Registry:           registry,
		Internet:           internetConnector,
		Relay:              relay,
		NetworkPreferences: networkPreferences,
		Storage:            db,
		WebPush:            webPush,
		Reminders:          reminderScheduler,
	}
	if cfg.Configure != nil {
		if err := cfg.Configure(runtime); err != nil {
			return fmt.Errorf("configure extensions: %w", err)
		}
	}
	if err := router.Register(server, router.RegisterConfig{
		BuildVersion:       cfg.BuildVersion,
		Store:              store,
		Registry:           registry,
		Internet:           internetConnector,
		Relay:              relay,
		NetworkPreferences: networkPreferences,
		Storage:            db,
		WebPush:            webPush,
		Reminders:          reminderScheduler,
		MessageRoute:       runtime.messageRoute,
		USSDRoute:          runtime.ussdRoute,
		ModemOverview:      runtime.modemOverview,
		Features:           runtime.features,
		Extensions:         runtime.routes,
	}); err != nil {
		return fmt.Errorf("configure router: %w", err)
	}

	var wg sync.WaitGroup

	wg.Go(func() {
		if err := reminderScheduler.Run(ctx); err != nil {
			slog.Error("reminder runner stopped", "error", err)
			stop()
		}
	})

	wg.Go(func() {
		if err := modem.RunEnableDisabled(ctx, registry, enableDisabledPolicy); err != nil {
			slog.Error("modem enable runner stopped", "error", err)
		}
	})

	wg.Go(func() {
		if err := modem.RunSMSStorageDefaults(ctx, registry, modem.SMSStorageME); err != nil {
			slog.Error("SMS storage defaults stopped", "error", err)
		}
	})

	wg.Go(func() {
		internetConnector.RunAlwaysOn(ctx, registry)
	})

	wg.Go(func() {
		if err := networkPreferences.Run(ctx, registry); err != nil {
			slog.Error("network preferences restore stopped", "error", err)
		}
	})

	wg.Go(func() {
		if err := hnetwork.RunRegistrationRestore(ctx, registry, db); err != nil {
			slog.Error("network registration restore stopped", "error", err)
		}
	})

	wg.Go(func() {
		if err := relay.Run(ctx); err != nil {
			slog.Error("message relay stopped", "error", err)
			stop()
		}
	})

	for _, runner := range runtime.runners {
		wg.Go(func() {
			if err := runner(ctx); err != nil {
				slog.Error("extension runner stopped", "error", err)
				stop()
			}
		})
	}

	startConfig := echo.StartConfig{
		Address:         cfg.ListenAddress,
		HideBanner:      true,
		GracefulTimeout: 5 * time.Second,
	}
	if err := startConfig.Start(ctx, server); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("start http server: %w", err)
	}
	wg.Wait()
	return nil
}

func resolveDBPath(path string) (string, error) {
	if path != "" {
		if filepath.IsAbs(path) {
			return path, nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve db path: %w", err)
		}
		return abs, nil
	}
	dataDir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "sigmo", "sigmo.db"), nil
}

func dataDir() (string, error) {
	if value := os.Getenv("XDG_DATA_HOME"); value != "" {
		if !filepath.IsAbs(value) {
			return "", fmt.Errorf("XDG_DATA_HOME %q is relative", value)
		}
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	if home == "" {
		return "", errors.New("user home dir is empty")
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("user home dir %q is relative", home)
	}
	return filepath.Join(home, ".local", "share"), nil
}

func applyLogLevel(debug bool) {
	if debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
		return
	}
	slog.SetLogLoggerLevel(slog.LevelInfo)
}

func newInternetConnector(store *settings.Store, db *storage.Store, networkPreferences *modem.NetworkPreferences) (*internet.Connector, error) {
	proxyConfig := store.ProxySettings()
	proxy := internet.NewProxy(internet.ProxyConfig{
		ListenAddress: proxyConfig.ListenAddress,
		HTTPPort:      proxyConfig.HTTPPort,
		SOCKS5Port:    proxyConfig.SOCKS5Port,
		Password:      proxyConfig.Password,
	})
	return internet.NewConnector(internet.ConnectorConfig{
		Proxy:              proxy,
		State:              db,
		NetworkPreferences: networkPreferences,
	})
}

func recoverInternetConnections(ctx context.Context, registry *modem.Registry, connector *internet.Connector) error {
	modemMap, err := registry.Modems(ctx)
	if err != nil {
		return fmt.Errorf("list modems: %w", err)
	}
	return connector.Recover(ctx, slices.Collect(maps.Values(modemMap)))
}
