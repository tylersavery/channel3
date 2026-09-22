// Command channel3 is the Channel Three broadcast appliance.
//
// It dispatches to one of three subcommands: serve, ingest and guide.
// Each subcommand lives in its own file and owns its own flags, so this file is
// settled: a later phase adds flags to its own subcommand file, never here.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

// version is replaced at build time with -ldflags "-X main.version=...".
var version = "dev"

// defaultRoot is where channel config and the media library live on the Pi.
// Nothing under it belongs to this repository.
const defaultRoot = "/srv/channel3"

// rootEnv names the environment variable that overrides defaultRoot.
const rootEnv = "CHANNEL3_ROOT"

// globals holds the flags every subcommand shares.
type globals struct {
	root string
}

// flagSet returns a flag set for a subcommand with the global flags already
// registered, so --root is accepted before or after the command name.
// A subcommand adds its own flags to the returned set and then calls parseFlags.
func (g *globals) flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("channel3 "+name, flag.ContinueOnError)
	fs.StringVar(&g.root, "root", g.root, "config and library root")
	return fs
}

// command is one subcommand of channel3.
type command struct {
	name    string
	summary string
	run     func(g *globals, args []string) error
}

// commands lists every subcommand in the order usage prints them.
func commands() []command {
	return []command{
		{"serve", "run the broadcast service", runServe},
		{"ingest", "download the configured sources into the library", runIngest},
		{"guide", "print the schedule as text", runGuide},
	}
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches args and returns the process exit status.
func run(args []string) int {
	g := &globals{root: rootDefault()}

	fs := flag.NewFlagSet("channel3", flag.ContinueOnError)
	fs.StringVar(&g.root, "root", g.root, "config and library root")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = usage

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *showVersion {
		fmt.Fprintf(os.Stdout, "channel3 %s\n", version)
		return 0
	}

	rest := fs.Args()
	if len(rest) == 0 {
		usage()
		return 2
	}

	for _, c := range commands() {
		if c.name == rest[0] {
			return exitCode(c.run(g, rest[1:]))
		}
	}

	fmt.Fprintf(os.Stderr, "channel3: unknown command %q\n\n", rest[0])
	usage()
	return 2
}

// rootDefault resolves the default root from the environment.
func rootDefault() string {
	if v := os.Getenv(rootEnv); v != "" {
		return v
	}
	return defaultRoot
}

// usage prints the command summary to stderr.
func usage() {
	w := os.Stderr
	fmt.Fprintf(w, "channel3 %s: broadcast TV for kids\n\n", version)
	fmt.Fprintf(w, "usage: channel3 [global flags] <command> [command flags]\n\ncommands:\n")
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-8s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(w, "\nglobal flags:\n")
	fmt.Fprintf(w, "  --root path   config and library root (default %q, or $%s)\n", defaultRoot, rootEnv)
	fmt.Fprintf(w, "  --version     print the version and exit\n")
	fmt.Fprintf(w, "\nGlobal flags may also follow the command name.\n")
}

// exitError carries the exit status a subcommand wants. A nil err means the
// problem was already reported and nothing more should be printed.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

// parseFlags parses a subcommand's flags. The flag package has already reported
// anything that went wrong, so the returned error only carries the exit status:
// 0 when help was requested, 1 for a usage error.
func parseFlags(fs *flag.FlagSet, args []string) error {
	err := fs.Parse(args)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, flag.ErrHelp):
		return &exitError{code: 0}
	default:
		return &exitError{code: 1}
	}
}

// notImplemented is returned by a subcommand stub that a later phase fills in.
func notImplemented(name string) error {
	return &exitError{code: 2, err: fmt.Errorf("%s: not implemented in this phase", name)}
}

// exitCode reports err and returns the process exit status for it: 0 for nil,
// the status carried by an exitError, and 1 for anything else.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		if ee.err != nil {
			fmt.Fprintf(os.Stderr, "channel3: %v\n", ee.err)
		}
		return ee.code
	}
	fmt.Fprintf(os.Stderr, "channel3: %v\n", err)
	return 1
}
