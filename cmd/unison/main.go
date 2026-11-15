// Command unison is the command line client of the daemon.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is set at build time from the VERSION file.
var version = "dev"

func main() {
	flag.Usage = func() {
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "usage: unison [--version]\n")
	}

	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if !*showVersion {
		flag.Usage()
		os.Exit(2)
	}

	fmt.Printf("unison %s\n", version)
}
