package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
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
	"golang.org/x/sync/errgroup"

	"github.com/damonto/sigmo/internal/app/buildinfo"
	"github.com/damonto/sigmo/internal/app/forwarder"
	hnetwork "github.com/damonto/sigmo/internal/app/handler/network"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/app/mcpauth"
	"github.com/damonto/sigmo/internal/app/mcpserver"
	"github.com/damonto/sigmo/internal/app/router"
	appupdate "github.com/damonto/sigmo/internal/app/update"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modemtask"
	"github.com/damonto/sigmo/internal/pkg/networkprefs"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/validator"
	"github.com/damonto/sigmo/internal/pkg/webpush"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

type Config struct {
	Build         buildinfo.Info
	ListenAddress string
	DBPath        string
	Debug         bool
	Authorize     AuthorizationFactory
	Configure     Extension
}

var (
	ErrRestart      = errors.New("restart requested")
	errSystemSignal = errors.New("system signal received")
)

const serverShutdownTimeout = 30 * time.Second

func Run(ctx context.Context, cfg Config) (runErr error) {
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = "0.0.0.0:9527"
	}
	applyLogLevel(cfg.Debug)
	httpapi.SetExposeInternalErrors(cfg.Debug)

	appCtx, cancelApp := context.WithCancelCause(ctx)
	signalCh := make(chan os.Signal, 1)
	stopSignalWatcher := make(chan struct{})
	signalWatcherDone := make(chan struct{})
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer close(signalWatcherDone)
		select {
		case <-signalCh:
			// Restore the default behavior before shutdown so a second signal
			// still terminates a process stuck in cleanup.
			signal.Stop(signalCh)
			cancelApp(errSystemSignal)
		case <-stopSignalWatcher:
		}
	}()
	runners, ctx := errgroup.WithContext(appCtx)
	var (
		cleanups         cleanupStack
		backgroundTasks  sync.WaitGroup
		runnerFailures   []error
		runnerFailuresMu sync.Mutex
	)
	defer func() {
		shutdownCause := context.Cause(ctx)
		cancelApp(context.Canceled)
		close(stopSignalWatcher)
		signal.Stop(signalCh)
		<-signalWatcherDone
		runnerWaitErr := runners.Wait()
		runnerFailuresMu.Lock()
		joinedRunnerFailures := errors.Join(runnerFailures...)
		runnerFailuresMu.Unlock()
		if joinedRunnerFailures != nil {
			runnerWaitErr = joinedRunnerFailures
		}
		runErr = errors.Join(runErr, runnerWaitErr)
		backgroundTasks.Wait()
		cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), serverShutdownTimeout)
		runErr = errors.Join(runErr, cleanups.Close(cleanupCtx))
		cancelCleanup()
		runErr = finalizeRunError(runErr, shutdownCause)
	}()
	restart := func() {
		cancelApp(ErrRestart)
	}

	resolvedDBPath, err := resolveDBPath(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("configure storage: %w", err)
	}
	db, err := storage.Open(ctx, resolvedDBPath)
	if err != nil {
		return fmt.Errorf("configure storage: %w", err)
	}
	cleanups.Add(func(context.Context) error {
		if err := db.Close(); err != nil {
			return fmt.Errorf("close storage: %w", err)
		}
		return nil
	})

	store, err := settings.NewStore(ctx, db)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	var authorization Authorization
	if cfg.Authorize != nil {
		authorization, err = cfg.Authorize(ctx, AuthorizationConfig{
			Build:    cfg.Build,
			Restart:  restart,
			StateDir: filepath.Dir(resolvedDBPath),
			Store:    store,
			Storage:  db,
		})
		if err != nil {
			return fmt.Errorf("configure authorization: %w", err)
		}
		startupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = authorization.Start(startupCtx)
		cancel()
		if err != nil {
			slog.Warn("validate product authorization", "error", err)
		}
		if !authorization.Authorized() {
			return runActivationServer(ctx, cfg, authorization)
		}
	}

	webPush, err := webpush.New(ctx, db)
	if err != nil {
		return fmt.Errorf("configure web push: %w", err)
	}
	reminderScheduler, err := reminder.New(db, store, webPush)
	if err != nil {
		return fmt.Errorf("configure reminders: %w", err)
	}
	slog.Info("server starting", "version", cfg.Build.Version, "edition", cfg.Build.Edition, "channel", cfg.Build.Channel, "listen_address", cfg.ListenAddress, "db_path", resolvedDBPath)

	registry, err := modem.NewRegistry()
	if err != nil {
		return fmt.Errorf("connect modem registry: %w", err)
	}
	cleanups.Add(func(context.Context) error {
		if err := registry.Close(); err != nil {
			return fmt.Errorf("close modem registry: %w", err)
		}
		return nil
	})
	err = registry.Start(ctx)
	if err != nil {
		return fmt.Errorf("start modem registry: %w", err)
	}

	server := echo.New()
	server.Logger = slog.Default()
	server.Validator, err = validator.New()
	if err != nil {
		return fmt.Errorf("configure request validator: %w", err)
	}
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
	networkPreferences, err := networkprefs.New(db)
	if err != nil {
		return fmt.Errorf("configure network preferences: %w", err)
	}
	enableDisabledPolicy := modemtask.EnableDisabledPolicy(networkprefs.SkipEnableDisabledInAirplaneMode(networkPreferences))
	internetConnector, err := newInternetConnector(internetConnectorConfig{
		settings:           store,
		state:              db,
		networkPreferences: networkPreferences,
		registry:           registry,
	})
	if err != nil {
		return fmt.Errorf("configure internet connector: %w", err)
	}
	unsubscribeInternetLifecycle, err := registry.Subscribe(ctx, func(event modem.ModemEvent) error {
		var previous *modem.Modem
		switch event.Type {
		case modem.ModemEventChanged:
			previous = event.Previous
		case modem.ModemEventRemoved, modem.ModemEventSIMChanged:
			previous = event.Modem
		}
		if previous == nil {
			return nil
		}
		return internetConnector.InvalidateModem(ctx, previous)
	})
	if err != nil {
		return fmt.Errorf("subscribe Internet modem lifecycle: %w", err)
	}
	cleanups.Add(func(context.Context) error {
		unsubscribeInternetLifecycle()
		return nil
	})
	startupCtx, cancelStartup := context.WithTimeout(ctx, 15*time.Second)
	if err := modemtask.EnableDisabled(startupCtx, registry, enableDisabledPolicy); err != nil {
		slog.Error("enable disabled modems", "error", err)
	}
	if err := recoverInternetConnections(startupCtx, registry, internetConnector); err != nil {
		slog.Error("recover internet connections", "error", err)
	}
	cancelStartup()
	lpaClients, err := lpa.NewPool(ctx, store, registry)
	if err != nil {
		return fmt.Errorf("configure LPA client pool: %w", err)
	}
	cleanups.Add(func(ctx context.Context) error {
		if err := lpaClients.Close(ctx); err != nil {
			return fmt.Errorf("close LPA client pool: %w", err)
		}
		return nil
	})
	relay, err := forwarder.New(store, registry, db, webPush)
	if err != nil {
		return fmt.Errorf("configure message relay: %w", err)
	}
	runtime := &Runtime{
		Store:                 store,
		Registry:              registry,
		InternetConnector:     internetConnector,
		LPAClients:            lpaClients,
		internetConnections:   internetConnector,
		Relay:                 relay,
		NetworkPreferences:    networkPreferences,
		Storage:               db,
		WebPush:               webPush,
		Reminders:             reminderScheduler,
		License:               authorization,
		airplaneModeLifecycle: internetConnector,
	}
	cleanups.Add(func(ctx context.Context) error {
		if err := runtime.close(ctx); err != nil {
			return fmt.Errorf("close extensions: %w", err)
		}
		return nil
	})
	if cfg.Configure != nil {
		if err := cfg.Configure(ctx, runtime); err != nil {
			return fmt.Errorf("configure extensions: %w", err)
		}
	}
	if runtime.UpdateSource == nil {
		runtime.UpdateSource = appupdate.NewGitHubSource(nil, "", cfg.Build.ReleasePublicKey)
	}
	updateController, err := appupdate.NewController(appupdate.ControllerConfig{
		Build:    cfg.Build,
		Settings: store,
		Source:   runtime.UpdateSource,
		License:  runtime.License,
		Restart:  restart,
	})
	if err != nil {
		return fmt.Errorf("configure updater: %w", err)
	}
	networkHandler, err := hnetwork.New(hnetwork.Config{
		Registry:              registry,
		Preferences:           networkPreferences,
		Store:                 db,
		AirplaneModeLifecycle: runtime.airplaneModeLifecycle,
	})
	if err != nil {
		return fmt.Errorf("configure network handler: %w", err)
	}
	cleanups.Add(func(context.Context) error {
		networkHandler.Close()
		return nil
	})
	mcpKeys, err := mcpauth.NewStore(db)
	if err != nil {
		return fmt.Errorf("configure MCP API keys: %w", err)
	}
	mcpCatalog := mcpserver.NewCatalog()
	if err := mcpserver.RegisterCoreTools(mcpCatalog, mcpserver.CoreToolsConfig{
		Store:               store,
		Registry:            registry,
		InternetConnector:   internetConnector,
		LPAClients:          lpaClients,
		InternetConnections: runtime.internetConnections,
		Relay:               relay,
		Network:             networkHandler,
		Storage:             db,
		Reminders:           reminderScheduler,
		MessageRoute:        runtime.messageRoute,
		USSDRoute:           runtime.ussdRoute,
		ModemOverview:       runtime.modemOverview,
	}); err != nil {
		return fmt.Errorf("configure MCP core tools: %w", err)
	}
	for _, extension := range runtime.mcpTools {
		if err := extension(mcpCatalog); err != nil {
			return fmt.Errorf("configure MCP extension: %w", err)
		}
	}
	mcpController, err := mcpserver.New(mcpserver.Config{
		BuildVersion: cfg.Build.Version,
		Settings:     store,
		Keys:         mcpKeys,
		Storage:      db,
		Catalog:      mcpCatalog,
	})
	if err != nil {
		return fmt.Errorf("configure MCP server: %w", err)
	}
	cleanups.Add(func(context.Context) error {
		mcpController.Close()
		return nil
	})
	if err := router.Register(server, router.RegisterConfig{
		Build:               cfg.Build,
		Store:               store,
		Registry:            registry,
		InternetConnector:   internetConnector,
		LPAClients:          lpaClients,
		InternetConnections: runtime.internetConnections,
		Relay:               relay,
		Network:             networkHandler,
		Storage:             db,
		WebPush:             webPush,
		Reminders:           reminderScheduler,
		MessageRoute:        runtime.messageRoute,
		USSDRoute:           runtime.ussdRoute,
		ModemOverview:       runtime.modemOverview,
		Features:            runtime.features,
		Extensions:          runtime.routes,
		MCP:                 mcpController,
		MCPKeys:             mcpKeys,
		Updates:             updateController,
		LicenseRoutes: func(group *echo.Group) {
			if authorization != nil {
				authorization.RegisterStatusRoute(group)
			}
		},
	}); err != nil {
		return fmt.Errorf("configure router: %w", err)
	}

	startRunner := func(name string, runner Runner) {
		runners.Go(func() error {
			err := superviseRunner(ctx, name, runner)
			if err != nil {
				runnerFailuresMu.Lock()
				runnerFailures = append(runnerFailures, err)
				runnerFailuresMu.Unlock()
			}
			return err
		})
	}

	startRunner("update", updateController.Run)

	if authorization != nil {
		startRunner("authorization", authorization.Run)
	}

	startRunner("reminder", reminderScheduler.Run)
	startRunner("modem enable", func(ctx context.Context) error {
		return modemtask.RunEnableDisabled(ctx, registry, enableDisabledPolicy)
	})
	startRunner("sms storage defaults", func(ctx context.Context) error {
		return modemtask.RunSMSStorageDefaults(ctx, registry, wwanmodem.MessageStorageDevice)
	})
	startRunner("always-on internet", func(ctx context.Context) error {
		internetConnector.RunAlwaysOn(ctx, registry)
		return nil
	})
	startRunner("network preferences restore", func(ctx context.Context) error {
		return modemtask.Run(ctx, registry, networkPreferences.Restore)
	})
	startRunner("network registration restore", func(ctx context.Context) error {
		return hnetwork.RunRegistrationRestore(ctx, registry, db)
	})
	startRunner("message relay", relay.Run)

	for i, runner := range runtime.runners {
		startRunner(fmt.Sprintf("extension %d", i+1), runner)
	}

	startConfig := echo.StartConfig{
		Address:         cfg.ListenAddress,
		HideBanner:      true,
		GracefulTimeout: 5 * time.Second,
		ListenerAddrFunc: func(net.Addr) {
			backgroundTasks.Go(func() {
				confirmUpdateHealthy(ctx, updateController.MarkHealthy)
			})
		},
	}
	if err := startConfig.Start(ctx, server); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("start http server: %w", err)
	}
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

type internetConnectorConfig struct {
	settings           *settings.Store
	state              *storage.Store
	networkPreferences *networkprefs.Store
	registry           *modem.Registry
}

func newInternetConnector(cfg internetConnectorConfig) (*internet.Connector, error) {
	proxyConfig := cfg.settings.ProxySettings()
	proxy := internet.NewProxy(internet.ProxyConfig{
		ListenAddress: proxyConfig.ListenAddress,
		HTTPPort:      proxyConfig.HTTPPort,
		SOCKS5Port:    proxyConfig.SOCKS5Port,
		Password:      proxyConfig.Password,
	})
	return internet.NewConnector(internet.ConnectorConfig{
		Proxy:              proxy,
		State:              cfg.state,
		NetworkPreferences: cfg.networkPreferences,
		Registry:           cfg.registry,
	})
}

func recoverInternetConnections(ctx context.Context, registry *modem.Registry, connector *internet.Connector) error {
	modemMap, err := registry.Modems(ctx)
	if err != nil {
		return fmt.Errorf("list modems: %w", err)
	}
	return connector.Recover(ctx, slices.Collect(maps.Values(modemMap)))
}
