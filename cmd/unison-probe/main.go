// Command unison-probe reports the state of the platform services Unison needs.
//
// Every mode registers itself, so a new area adds a file without touching this
// one.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
)

// version is set at build time from the VERSION file.
var version = "dev"

// diagnostic is one mode of the probe.
type diagnostic struct {
	name string
	run  func(context.Context, []string) error
}

// diagnostics holds the registered modes.
var diagnostics []diagnostic

// register adds a mode to the probe.
func register(name string, run func(context.Context, []string) error) {
	diagnostics = append(diagnostics, diagnostic{name: name, run: run})
}

func main() {
	flag.Usage = usage
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	run := lookup(args[0])
	if run == nil {
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", args[0])
		usage()
		os.Exit(2)
	}
	if err := run(context.Background(), args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", args[0], err)
		os.Exit(1)
	}
}

// usage prints the modes of the probe.
func usage() {
	_, _ = fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [-version] <mode> [flags]\n\nmodes:\n", os.Args[0])
	for _, name := range names() {
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "  %s\n", name)
	}
}

// names returns the registered mode names in order.
func names() []string {
	out := make([]string, 0, len(diagnostics))
	for _, mode := range diagnostics {
		out = append(out, mode.name)
	}
	sort.Strings(out)
	return out
}

// lookup returns the mode with the name, or nil.
func lookup(name string) func(context.Context, []string) error {
	for _, mode := range diagnostics {
		if mode.name == name {
			return mode.run
		}
	}
	return nil
}

// status prints a line of the report.
func status(label, value string) {
	fmt.Printf("%s: %s\n", label, value)
}

// yesNo returns yes or no for a check.
func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
