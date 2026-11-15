// Command unisond runs the Unison daemon.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/internal/daemon"
)

// version is set at build time from the VERSION file.
var version = "dev"

// Clipboard modes of the daemon.
const (
	clipboardSystem = "system"
	clipboardNone   = "none"
)

// Exit codes of the command.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run runs the daemon and returns the exit code.
func run(args []string) int {
	flags := flag.NewFlagSet("unisond", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() {
		out := flags.Output()
		_, _ = fmt.Fprintln(out, "usage: unisond [flags]")
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "flags:")
		flags.PrintDefaults()
	}

	showVersion := flags.Bool("version", false, "print the version and exit")
	dataDir := flags.String("data-dir", "", "data directory of this instance")
	endpoint := flags.String("endpoint", "", "local API endpoint of this instance")
	clipboard := flags.String("clipboard", clipboardSystem, "clipboard to use: system or none")
	name := flags.String("name", "", "device name announced to peers")
	level := flags.String("log-level", "info", "log level: debug, info, warn or error")
	stderr := flags.Bool("log-stderr", false, "copy the log to stderr")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if *showVersion {
		_, _ = fmt.Printf("unisond %s\n", version)
		return exitOK
	}

	logLevel, err := parseLevel(*level)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "unisond: "+err.Error())
		return exitUsage
	}
	mode := strings.ToLower(strings.TrimSpace(*clipboard))
	if mode != clipboardSystem && mode != clipboardNone {
		_, _ = fmt.Fprintf(os.Stderr, "unisond: unknown clipboard %q, use %s or %s\n", *clipboard, clipboardSystem, clipboardNone)
		return exitUsage
	}

	dir, err := daemon.ResolveDataDir(*dataDir)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "unisond: "+err.Error())
		return exitError
	}
	keystore, err := daemon.NewKeystore(dir)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "unisond: "+err.Error())
		return exitError
	}
	clip, err := newClipboard(mode)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "unisond: "+err.Error())
		return exitError
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	options := daemon.Options{
		DataDir:   dir,
		Endpoint:  *endpoint,
		Version:   version,
		Name:      *name,
		Platform:  daemon.Platform,
		Keystore:  keystore,
		Clipboard: clip,
		LogLevel:  logLevel,
	}
	if *stderr {
		options.Stderr = os.Stderr
	}
	if err := daemon.Run(ctx, options); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "unisond: "+err.Error())
		return exitError
	}
	return exitOK
}

// newClipboard returns the clipboard of the mode.
func newClipboard(mode string) (ports.Clipboard, error) {
	if mode == clipboardNone {
		return daemon.NoClipboard(), nil
	}
	return daemon.NewClipboard()
}

// parseLevel returns the log level of a name.
func parseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q, use debug, info, warn or error", name)
	}
}
