// Command unison-relay runs the relay server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/lebe-dev/unison/internal/relay"
)

// version is set at build time from the VERSION file.
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "unison-relay: %v\n", err)
		os.Exit(1)
	}
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
		return err
	}
	if err := server.Start(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	return server.Close()
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
