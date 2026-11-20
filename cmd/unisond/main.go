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
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/lebe-dev/unison/internal/daemon"
	"github.com/lebe-dev/unison/internal/observability"
)

// version is set at build time from the VERSION file.
var version = "dev"

// flushTimeout bounds the wait for the queued reports when the daemon exits.
const flushTimeout = 5 * time.Second

// Exit codes of the command.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	observability.Init(observability.Options{Component: "daemon", Version: version})
	os.Exit(runReported())
}

// runReported runs the command and returns its exit code. A panic of this
// goroutine is reported and raised again, so the process still crashes.
func runReported() (code int) {
	defer func() {
		if value := recover(); value != nil {
			observability.CapturePanic(value)
			observability.Flush(flushTimeout)
			panic(value)
		}
	}()

	code = run(os.Args[1:])
	observability.Flush(flushTimeout)
	return code
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
	clipboard := flags.String("clipboard", daemon.DefaultClipboardMode, "clipboard to use: "+strings.Join(daemon.ClipboardModes, ", "))
	name := flags.String("name", "", "device name announced to peers")
	level := flags.String("log-level", "info", "log level: debug, info, warn or error")
	stderr := flags.Bool("log-stderr", false, "copy the log to stderr")
	netEcho := flags.Bool("net-echo", false, "answer UDP probes of the network check")
	netEchoListen := flags.String("net-echo-listen", defaultNetEchoListen, "address of the UDP echo")
	netEchoPeer := flags.String("net-echo-peer", "", "probe this UDP address and exit")
	netEchoWait := flags.Duration("net-echo-wait", 0, "time to wait for a probe, unlimited when zero")

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	echo := netEchoOptions{listen: *netEchoListen, peer: *netEchoPeer, wait: *netEchoWait}
	if *netEcho || echo.peer != "" {
		return runNetEcho(ctx, echo, os.Stdout)
	}

	logLevel, err := parseLevel(*level)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "unisond: "+err.Error())
		return exitUsage
	}
	mode := strings.ToLower(strings.TrimSpace(*clipboard))
	if !slices.Contains(daemon.ClipboardModes, mode) {
		_, _ = fmt.Fprintf(os.Stderr, "unisond: unknown clipboard %q, use %s\n", *clipboard, strings.Join(daemon.ClipboardModes, ", "))
		return exitUsage
	}

	dir, err := daemon.ResolveDataDir(*dataDir)
	if err != nil {
		return fail(err)
	}
	keystore, err := daemon.NewKeystore(dir)
	if err != nil {
		return fail(err)
	}
	clip, err := daemon.NewClipboard(mode)
	if err != nil {
		return fail(err)
	}

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
		return fail(err)
	}
	return exitOK
}

// fail prints err, reports it and returns the exit code of a failure. A second
// instance on the data directory is an expected condition and is not reported.
func fail(err error) int {
	if !errors.Is(err, daemon.ErrRunning) {
		observability.CaptureException(err)
	}
	_, _ = fmt.Fprintln(os.Stderr, "unisond: "+err.Error())
	return exitError
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
