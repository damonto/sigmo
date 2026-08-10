package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"syscall"

	"github.com/damonto/sigmo/internal/app/buildinfo"
	"github.com/damonto/sigmo/internal/app/server"
	appupdate "github.com/damonto/sigmo/internal/app/update"
	prolicense "github.com/damonto/sigmo/pro/license"
)

var (
	listenAddress string
	dbPath        string
	debug         bool
	showVersion   bool
)

func init() {
	flag.StringVar(&listenAddress, "listen-address", "0.0.0.0:9527", "HTTP listen address")
	flag.StringVar(&dbPath, "db-path", "", "path to SQLite database")
	flag.BoolVar(&debug, "debug", false, "enable debug logging and internal error responses")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
}

func main() {
	flag.Parse()
	info := buildinfo.Current()
	info.Edition = buildinfo.EditionPro
	if showVersion {
		fmt.Println(info.Version)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		slog.Error("resolve executable", "error", err)
		os.Exit(1)
	}
	if restored, err := appupdate.RecoverPending(executable); err != nil {
		slog.Error("recover pending update", "error", err)
		os.Exit(1)
	} else if restored {
		if err := syscall.Exec(executable, os.Args, os.Environ()); err != nil {
			slog.Error("restart restored version", "error", err)
			os.Exit(1)
		}
	}
	err = server.Run(server.Config{
		Build:         info,
		ListenAddress: listenAddress,
		DBPath:        dbPath,
		Debug:         debug,
		Authorize:     authorizePro,
		Configure:     configurePro,
	})
	if errors.Is(err, server.ErrRestart) {
		if err := syscall.Exec(executable, os.Args, os.Environ()); err != nil {
			slog.Error("restart after update", "error", err)
			os.Exit(1)
		}
	}
	if err != nil {
		slog.Error("run server", "error", err)
		os.Exit(1)
	}
}

func authorizePro(ctx context.Context, cfg server.AuthorizationConfig) (server.Authorization, error) {
	return prolicense.New(ctx, prolicense.Config{
		BaseURL:          prolicense.WorkerURL,
		LicensePublicKey: prolicense.LicensePublicKey,
		ReleasePublicKey: cfg.Build.ReleasePublicKey,
		Storage:          cfg.Storage,
		Restart:          cfg.Restart,
	})
}

func configurePro(runtime *server.Runtime) error {
	if source, ok := runtime.License.(appupdate.Source); ok {
		runtime.SetUpdateSource(source)
	}
	app := &proApp{runtime: runtime}
	if proIMS != nil {
		if err := proIMS(app); err != nil {
			return err
		}
	}
	if proESIMTransfer != nil {
		if err := proESIMTransfer(app); err != nil {
			return err
		}
	}
	return nil
}

type proApp struct {
	runtime  *server.Runtime
	websheet websheetState
}
