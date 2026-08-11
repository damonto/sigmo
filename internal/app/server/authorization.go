package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/damonto/sigmo/internal/app/buildinfo"
	appupdate "github.com/damonto/sigmo/internal/app/update"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/web"
)

// Authorization is defined by the server, which consumes Pro authorization.
// The Pro module supplies the implementation without creating a dependency
// from the public module back to pro/.
type Authorization interface {
	appupdate.LicenseProvider
	Start(context.Context) error
	Authorized() bool
	RegisterActivationRoutes(*echo.Group)
	RegisterStatusRoute(*echo.Group)
	Run(context.Context) error
}

type AuthorizationConfig struct {
	Build    buildinfo.Info
	Restart  func()
	StateDir string
	Store    *settings.Store
	Storage  *storage.Store
}

type AuthorizationFactory func(context.Context, AuthorizationConfig) (Authorization, error)

const updateHealthConfirmationDelay = 5 * time.Second

func runActivationServer(ctx context.Context, cfg Config, authorization Authorization) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	server := echo.New()
	server.Logger = slog.Default()
	server.Use(middleware.RequestID())
	server.Use(middleware.Recover())
	server.Use(middleware.StaticWithConfig(middleware.StaticConfig{
		Filesystem: web.Root(),
		Index:      "index.html",
		HTML5:      true,
		Skipper: func(c *echo.Context) bool {
			return strings.HasPrefix(c.Request().URL.Path, "/api/")
		},
	}))

	v1 := server.Group("/api/v1")
	authorization.RegisterActivationRoutes(v1)

	var wg sync.WaitGroup
	defer func() {
		cancel()
		wg.Wait()
	}()
	startConfig := echo.StartConfig{
		Address:         cfg.ListenAddress,
		HideBanner:      true,
		GracefulTimeout: 5 * time.Second,
		ListenerAddrFunc: func(net.Addr) {
			wg.Go(func() {
				confirmUpdateHealthy(runCtx, func() error {
					return appupdate.MarkHealthy(executable)
				})
			})
		},
	}
	if err := startConfig.Start(runCtx, server); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("start activation server: %w", err)
	}
	return nil
}

func confirmUpdateHealthy(ctx context.Context, markHealthy func() error) {
	confirmUpdateHealthyAfter(ctx, updateHealthConfirmationDelay, markHealthy)
}

func confirmUpdateHealthyAfter(ctx context.Context, delay time.Duration, markHealthy func() error) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	if err := markHealthy(); err != nil {
		slog.Warn("mark updated binary healthy", "error", err)
	}
}
