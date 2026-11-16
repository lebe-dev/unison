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
	case "transfers":
		err = a.transfers(ctx, rest)
	case "friends":
		err = a.friends(ctx, rest)
	case "requests":
		err = a.requests(ctx, rest)
	case "pause":
		err = a.pause(ctx, rest)
	case "policy":
		err = a.policy(ctx, rest)
	case "settings":
		err = a.settings(ctx, rest)
	case "relay":
		err = a.relay(ctx, rest)
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

// parseCommand parses the flags of a command and returns its positional
// arguments. The flags may stand before, after or between the arguments.
func parseCommand(set *flag.FlagSet, args []string) ([]string, error) {
	flags, positional := splitFlags(args, valuedFlags(set))
	if err := parseFlags(set, flags); err != nil {
		return nil, err
	}
	return positional, nil
}

// parseFlags parses the arguments of a flag set and returns the error to report.
func parseFlags(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return errHandled
	}
	return nil
}

// valuedFlags returns the names of the flags of a set that take the next
// argument as their value.
func valuedFlags(set *flag.FlagSet) map[string]bool {
	valued := make(map[string]bool)
	set.VisitAll(func(f *flag.Flag) {
		if boolean, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && boolean.IsBoolFlag() {
			return
		}
		valued[f.Name] = true
	})
	return valued
}

// splitFlags moves the flags of a command line in front of the positional
// arguments and returns both groups. valued lists the flags that take the next
// argument as their value. Everything after a "--" is positional.
func splitFlags(args []string, valued map[string]bool) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return flags, append(positional, args[i+1:]...)
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
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
	return flags, positional
}

// subcommand returns the subcommand of a command line and its arguments. The
// flags of a command may come before the subcommand as well as after it, so the
// returned arguments keep the flags in front of the positional ones. A line
// without a subcommand returns an empty name with its flags only. The named
// flags take the next argument as their value.
func subcommand(args []string, valued ...string) (name string, rest []string) {
	takesValue := map[string]bool{"endpoint": true}
	for _, flagName := range valued {
		takesValue[flagName] = true
	}

	flags, positional := splitFlags(args, takesValue)
	if len(positional) == 0 {
		return "", flags
	}
	return positional[0], append(flags, positional[1:]...)
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
  devices rename <id|self> <name>
                              change the local name of a device, or the name
                              this device announces to the peers
  devices address <id> <host:port> [--wait 5s]
                              store an address of a paired device and connect
                              to it, for networks where discovery does not work
  devices confirm <id|self> on|off [--files] [--peer]
                              ask before taking the clipboard items (or with
                              --files the file transfers) of a device or of
                              every device; with --peer the paired device asks
                              before taking those of this device
  devices revoke <id|self> [--yes]
                              revoke a device or leave the group; the device is
                              cut off and never trusted again
  pair [--role personal|friends] [--qr <code>]
                              pair a device: print a square to scan, compare the
                              short code, or join the device that shows a code
  clip get [--from <id>]       print the last clipboard item, optionally of one
                              paired device
  clip set [-]                copy standard input to the clipboard
  history [ls] [--type text|image|file] [--limit N]
                              list the clipboard history
  history fetch <id> [-o path]
                              download the payloads of a history item
  history apply <id>          apply a held history item to the clipboard
  send <path|-> --to <device>...
                              offer a file or standard input to devices
  transfers [ls]              list the file transfers of this device
  transfers cancel <id>       cancel a transfer
  friends search [--off]      look for Friends devices for a limited time; both
                              devices have to turn it on
  requests [ls]               list the transfer requests that wait for a decision
  requests accept <id>        accept a request and receive its files
  requests decline <id>       decline a request
  pause [5m|1h|on|off]        pause or resume the clipboard synchronization
  policy                      show what arrives by itself on every kind of
                              network
  settings                    show the settings of the daemon
  settings ttl                show the lifetimes of the history
  settings ttl <kind> <lifetime>
                              change one lifetime; the kind is text, image or
                              files and the lifetime looks like 24h or 7d
  settings ttl reset          back to the default lifetimes
  relay [status]              show the relay of the group and its connection
  relay set <url> --token <t> [--files-on-request]
                              store the relay address and token; the other
                              Personal devices receive them
  relay off                   turn the relay off for the group

<id> is a unique prefix of the fingerprint or the id of a device, or its
name. The status command prints the addresses of this device; the address of a
peer is host:port from that output, with an IPv6 host in brackets.

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
