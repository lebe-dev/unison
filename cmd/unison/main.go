// Command unison is the command line client of the daemon. It speaks the local
// API over the socket or the named pipe of the daemon.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api/localconn"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// version is set at build time from the VERSION file.
var version = "dev"

// Exit codes of the command.
const (
	// exitOK reports a command that ran to the end.
	exitOK = 0
	// exitFailure reports a failed command.
	exitFailure = 1
	// exitUnreachable reports a daemon that the command cannot reach.
	exitUnreachable = 2
)

// Errors a command reports for the exit code.
var (
	// errHandled marks a failure that has already been explained on stderr.
	errHandled = errors.New("command failed")
	// errHelp marks a command line that asked for the usage.
	errHelp = errors.New("help requested")
)

func main() {
	os.Exit(exec())
}

// exec runs the command line of the process.
func exec() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}

// run executes one command line and returns its exit code.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	app := &app{stdin: stdin, stdout: stdout, stderr: stderr}
	return app.run(ctx, args)
}

// app carries the streams, the options and the client of one command line.
type app struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	// endpoint overrides the endpoint of the daemon. Empty means the
	// environment or the platform default.
	endpoint string
	json     bool

	apiClient localv1connect.UnisonServiceClient
}

// run dispatches one command line.
func (a *app) run(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("unison", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	flags.StringVar(&a.endpoint, "endpoint", "", "path of the daemon endpoint")
	flags.BoolVar(&a.json, "json", false, "print machine readable JSON")
	showVersion := flags.Bool("version", false, "print the version and exit")
	flags.Usage = func() { a.usage(a.stderr) }

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitFailure
	}
	if *showVersion {
		_, _ = fmt.Fprintf(a.stdout, "unison %s\n", version)
		return exitOK
	}

	rest := flags.Args()
	if len(rest) == 0 {
		a.usage(a.stderr)
		return exitFailure
	}
	command, rest := rest[0], rest[1:]

	var err error
	switch command {
	case "status":
		err = a.status(ctx, rest)
	case "devices":
		err = a.devices(ctx, rest)
	case "pair":
		err = a.pair(ctx, rest)
	case "clip":
		err = a.clip(ctx, rest)
	case "history":
		err = a.history(ctx, rest)
	case "send":
		err = a.send(ctx, rest)
	case "requests":
		err = a.requests(ctx, rest)
	case "pause":
		err = a.pause(ctx, rest)
	case "help":
		a.usage(a.stdout)
		return exitOK
	default:
		_, _ = fmt.Fprintf(a.stderr, "unison: unknown command %q\n", command)
		a.usage(a.stderr)
		return exitFailure
	}
	if err != nil {
		return a.fail(err)
	}
	return exitOK
}

// fail reports a failed command and returns its exit code.
func (a *app) fail(err error) int {
	if errors.Is(err, errHelp) {
		return exitOK
	}
	if errors.Is(err, errHandled) {
		return exitFailure
	}
	if connect.CodeOf(err) == connect.CodeUnavailable {
		_, _ = fmt.Fprintf(a.stderr, "unison: the daemon is not reachable at %s: %v\n", a.endpoint, err)
		_, _ = fmt.Fprintf(a.stderr, "start the daemon or pass --endpoint\n")
		return exitUnreachable
	}
	_, _ = fmt.Fprintf(a.stderr, "unison: %v\n", err)
	return exitFailure
}

// parseFlags parses the arguments of a command and returns the error to report.
func parseFlags(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return errHandled
	}
	return nil
}

// splitFlags moves the flags of a command line before its positional arguments,
// so a command may take its arguments in any order. valued lists the flags that
// take the next argument as their value.
func splitFlags(args []string, valued map[string]bool) (flags, rest []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-" || arg == "--" || !strings.HasPrefix(arg, "-") {
			rest = append(rest, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") || !valued[name] || i+1 >= len(args) {
			continue
		}
		i++
		flags = append(flags, args[i])
	}
	return flags, rest
}

// newFlagSet returns the flag set of a command. The common flags may be given
// after the command as well.
func (a *app) newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet("unison "+name, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	flags.StringVar(&a.endpoint, "endpoint", a.endpoint, "path of the daemon endpoint")
	flags.BoolVar(&a.json, "json", a.json, "print machine readable JSON")
	flags.Usage = func() { a.usage(a.stderr) }
	return flags
}

// usageError reports an invalid command line and prints the usage.
func (a *app) usageError(format string, args ...any) error {
	_, _ = fmt.Fprintf(a.stderr, "unison: "+format+"\n", args...)
	a.usage(a.stderr)
	return errHandled
}

// usage prints the command line reference.
func (a *app) usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `usage: unison [--endpoint path] [--json] <command> [arguments]

commands:
  status                      show the daemon, the network and the peers
  devices [ls]                list the paired devices
  devices rename <id> <name>  change the name of a paired device
  devices confirm <id|self> on|off
                              hold the clipboard items of a device, or the items
                              of every device, until they are applied
  devices revoke <id|self> [--yes]
                              revoke a device or leave the group; the device is
                              cut off and never trusted again
  pair [--role personal|friends]
                              pair a device and compare the short code
  clip get [--from <id>]       print the last clipboard item, optionally of one
                              paired device
  clip set [-]                copy standard input to the clipboard
  history [ls]                list the clipboard history
  history fetch <id> [-o path]
                              download the payloads of a history item
  send <path|-> --to <device>...
                              offer a file or standard input to devices
  requests [ls]               list the transfer requests that wait for a decision
  requests accept <id>        accept a request and receive its files
  requests decline <id>       decline a request
  pause [5m|1h|on|off]        pause or resume the clipboard synchronization

<id> is a unique prefix of the fingerprint or the id of a device, or its
name.

options:
  --endpoint path  endpoint of the daemon, by default $UNISON_LOCAL_API or the
                   socket of the platform
  --json           print machine readable JSON
  --version        print the version and exit
`)
}

// api returns the client of the daemon and resolves the endpoint once.
func (a *app) api() (localv1connect.UnisonServiceClient, error) {
	if a.apiClient != nil {
		return a.apiClient, nil
	}
	path := a.endpoint
	if path == "" {
		resolved, err := localconn.DefaultPath()
		if err != nil {
			return nil, err
		}
		path = resolved
	}
	a.endpoint = path
	a.apiClient = localconn.NewClient(path)
	return a.apiClient, nil
}
