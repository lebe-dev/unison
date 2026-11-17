// Command unison-relay runs the relay server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lebe-dev/unison/internal/observability"
	"github.com/lebe-dev/unison/internal/relay"
)

// version is set at build time from the VERSION file.
var version = "dev"

// flushTimeout bounds the delivery of the queued events before the exit.
const flushTimeout = 5 * time.Second

func main() {
	initObservability()
	os.Exit(exitCode())
}

// exitCode runs the server and returns the process status. The defers run
// before os.Exit.
func exitCode() int {
	defer observability.Flush(flushTimeout)
	defer observability.Recover()

	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "unison-relay: %v\n", err)
		return 1
	}
	return 0
}

// initObservability reports always when a DSN is available, unless the
// environment turns reporting off.
func initObservability() {
	started := observability.Init(observability.Options{
		Component: "relay",
		Version:   version,
		DSN:       strings.TrimSpace(os.Getenv("SENTRY_DSN")),
	})
	if !started {
		return
	}
	observability.SetEnabled(reportingAllowed())
}

// reportingAllowed reports whether the environment leaves reporting on.
func reportingAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("UNISON_RELAY_SENTRY"))) {
	case "0", "false", "off":
		return false
	}
	return true
}

func run(args []string) error {
	if hasVersionFlag(args) {
		fmt.Printf("unison-relay %s\n", version)
		return nil
	}

	fs := flag.NewFlagSet("unison-relay", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "usage: unison-relay [flags]")
		_, _ = fmt.Fprintln(fs.Output(), "")
		_, _ = fmt.Fprintln(fs.Output(), "flags:")
		fs.PrintDefaults()
	}

	cfg, err := relay.ParseConfig(fs, args, os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cfg.Version = version

	server, err := relay.New(cfg)
	if err != nil {
		observability.CaptureException(err)
		return err
	}
	if err := server.Start(); err != nil {
		observability.CaptureException(err)
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	if err := server.Close(); err != nil {
		observability.CaptureException(err)
		return err
	}
	return nil
}

// hasVersionFlag reports whether the arguments ask for the version.
func hasVersionFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-version" || arg == "--version" {
			return true
		}
	}
	return false
}
