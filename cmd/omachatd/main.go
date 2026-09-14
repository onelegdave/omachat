// Command omachatd is the protocol helper for OmaChat.
// It speaks the Google Messages web protocol via libgm and exposes a
// newline-delimited JSON API on a Unix socket. The Omarchy shell owns this
// process; there is no systemd unit.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/onelegdave/omachat/internal/daemon"
	"github.com/onelegdave/omachat/internal/store"
)

// version is overridden at build time with -ldflags.
var version = "dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		logLevel    = flag.String("log-level", "info", "trace, debug, info, warn, error")
		socketPath  = flag.String("socket", "", "override the Unix socket path")
	)
	if len(os.Args) > 1 && os.Args[1] == "pair" {
		if err := runPair(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	level, err := zerolog.ParseLevel(*logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid log level %q\n", *logLevel)
		os.Exit(2)
	}
	log := zerolog.New(zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.Out = os.Stderr
		w.TimeFormat = time.RFC3339
	})).Level(level).With().Timestamp().Logger()

	if err := run(log, *socketPath); err != nil {
		log.Error().Err(err).Msg("Fatal")
		os.Exit(1)
	}
}

func run(log zerolog.Logger, socketOverride string) error {
	paths, err := store.NewPaths()
	if err != nil {
		return fmt.Errorf("resolve paths: %w", err)
	}
	socketPath := paths.SocketPath()
	if socketOverride != "" {
		socketPath = socketOverride
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d := daemon.New(log, paths)
	if err := d.Start(ctx); err != nil {
		return err
	}
	defer d.Stop()

	log.Info().Str("version", version).Msg("omachatd started")
	return d.Serve(ctx, socketPath)
}
