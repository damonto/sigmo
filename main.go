package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/damonto/sigmo/internal/app/server"
)

var (
	BuildVersion  string
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
	if showVersion {
		fmt.Println(BuildVersion)
		return
	}
	if err := server.Run(server.Config{
		BuildVersion:  BuildVersion,
		ListenAddress: listenAddress,
		DBPath:        dbPath,
		Debug:         debug,
	}); err != nil {
		slog.Error("run server", "error", err)
		os.Exit(1)
	}
}
