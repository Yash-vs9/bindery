// Command bindery turns a folder of Markdown into a searchable documentation
// site, with a live-reload development server, using only the Go standard
// library.
//
// The dependency manifest (go.mod) has no require block. Every capability that
// would normally arrive as a package -- the CommonMark parser, YAML
// front-matter, syntax highlighting, the WebSocket used for live reload, the
// file watcher, and the search index -- is implemented here. STDLIB.md records
// each substitution.
//
// Usage:
//
//	bindery dev    [dir]      serve a directory with live reload
//	bindery build  [dir]      render a directory to static HTML
//	bindery spec              report CommonMark conformance
//	bindery render FILE.md    render one file to stdout
//	bindery version           print version information
package main

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// version is a compile-time constant rather than a linker-injected build stamp
// so that two builds of the same source produce byte-identical binaries. See
// the Reproducible Build section of README.md.
const version = "0.1.0-dev"

// Exit codes. Anything a shell script might branch on gets a stable number.
const (
	exitOK      = 0 // success
	exitFailure = 1 // the command ran and reported a problem
	exitUsage   = 2 // the command line itself was wrong
)

// errUsage signals that the process should exit with exitUsage. Returning it
// keeps flag parsing failures on the same path as every other error.
var errUsage = errors.New("usage")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main's testable core: it takes an argument list and its streams
// explicitly, and returns an exit code instead of calling os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}

	var err error
	switch cmd := args[0]; cmd {
	case "dev":
		err = cmdDev(args[1:], stdout, stderr)
	case "build":
		err = cmdBuild(args[1:], stdout, stderr)
	case "spec":
		err = cmdSpec(args[1:], stdout, stderr)
	case "render":
		err = cmdRender(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "bindery %s (%s %s/%s)\n",
			version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return exitOK
	case "help", "-h", "--help":
		usage(stdout)
		return exitOK
	default:
		fmt.Fprintf(stderr, "bindery: unknown command %q\n", cmd)
		usage(stderr)
		return exitUsage
	}

	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errUsage):
		// flag.ContinueOnError has already printed the specific complaint.
		return exitUsage
	default:
		fmt.Fprintf(stderr, "bindery: %v\n", err)
		return exitFailure
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `bindery - a documentation site generator with no dependencies

usage: bindery <command> [flags] [arguments]

commands:
  dev    [dir]       serve dir at localhost with live reload (default ".")
                     flags: --port
  build  [dir]       render dir to static HTML (default ".")
  spec               run the CommonMark conformance suite and report the score
  render FILE.md     render a single file to stdout
  version            print version information
  help               print this message

run "bindery <command> -h" for the flags of a single command
`)
}

// newFlagSet returns a flag set that reports errors on stderr and never calls
// os.Exit, so that every command exits through run.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("bindery "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parse permutes args so that flags may follow positional arguments, then parses.
//
// This is where the standard library stops. Package flag stops parsing at the
// first non-flag argument, by design, so "bindery build docs --out site" would
// treat --out as a positional argument and silently ignore it. Every CLI users
// expect to behave otherwise -- git, go, and anything built on the packages
// people reach for -- permutes first. Twenty lines here buys that.
//
// Whether a flag consumes the following argument is asked of the flag set
// itself: a boolean flag does not, anything else does. flag.Value does not
// expose that, but the boolFlag convention package flag uses internally is
// visible through a type assertion.
func parse(fs *flag.FlagSet, args []string) error {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case len(arg) > 1 && arg[0] == '-':
			flags = append(flags, arg)
			name := strings.TrimLeft(arg, "-")
			if strings.ContainsRune(name, '=') {
				continue // --flag=value carries its own value
			}
			if f := fs.Lookup(name); f != nil && !isBoolFlag(f) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			positional = append(positional, arg)
		}
	}
	return fs.Parse(append(flags, positional...))
}

// isBoolFlag reports whether f is a boolean flag, which takes no value.
func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// errNotImplemented is returned by commands whose implementation has not landed
// yet. It keeps the CLI surface honest: the command exists, exits non-zero, and
// says so, rather than pretending to succeed.
type errNotImplemented struct {
	cmd       string
	milestone string
}

func (e errNotImplemented) Error() string {
	return fmt.Sprintf("%s is not implemented yet (milestone %s)", e.cmd, e.milestone)
}

func cmdDev(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("dev", stderr)
	port := fs.Int("port", 8080, "port to listen on")
	if err := parse(fs, args); err != nil {
		return errUsage
	}

	root := dirArg(fs)
	srv, err := NewServer(root)
	if err != nil {
		return err
	}
	defer srv.Close()

	// signal.NotifyContext turns ctrl-c into a cancelled context, which is the
	// same shutdown path the tests use. No separate signal handling.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr, wait, err := srv.Serve(ctx, fmt.Sprintf(":%d", *port))
	if err != nil {
		return err
	}

	watcher := DefaultWatcher(root, func() {
		start := time.Now()
		if err := srv.Rebuild(); err != nil {
			// A broken file must not blank the browser: report it and keep
			// serving the last good build.
			fmt.Fprintf(stderr, "%s %v\n", cross(stderr), err)
			return
		}
		srv.hub.Broadcast("reload")
		pages := len(srv.Site().Pages)
		fmt.Fprintf(stdout, "%s rebuilt %d page%s in %s %s\n",
			tick(stdout), pages, plural(pages),
			time.Since(start).Round(time.Millisecond),
			dim(stdout, fmt.Sprintf("(%d browser%s connected)",
				srv.hub.Clients(), plural(srv.hub.Clients()))))
	})
	go watcher.Run(ctx)

	site := srv.Site()
	fmt.Fprintf(stdout, "%s %d page%s on %s\n",
		tick(stdout), len(site.Pages), plural(len(site.Pages)),
		style(stdout, fmt.Sprintf("http://localhost:%d", addr.Port), ansiCyan, ansiBold))
	fmt.Fprintf(stdout, "  %s\n", dim(stdout, fmt.Sprintf(
		"watching %s every %s, ctrl-c to stop", root, watcher.Poll)))
	return wait()
}

func cmdBuild(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("build", stderr)
	out := fs.String("out", "site", "directory to write the rendered site to")
	if err := parse(fs, args); err != nil {
		return errUsage
	}

	start := time.Now()
	site, err := LoadSite(dirArg(fs), false)
	if err != nil {
		return err
	}
	if err := site.Build(*out, false); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s %d page%s in %s -> %s\n",
		tick(stdout), len(site.Pages), plural(len(site.Pages)),
		time.Since(start).Round(time.Millisecond), *out)
	return nil
}

func cmdSpec(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("spec", stderr)
	verbose := fs.Bool("verbose", false, "show a diff for each failing example")
	section := fs.String("section", "", "only run examples from this spec section")
	if err := parse(fs, args); err != nil {
		return errUsage
	}

	examples, err := loadSpec()
	if err != nil {
		return err
	}
	result := runSpec(examples, *section)
	if result.Total == 0 {
		return fmt.Errorf("no examples in section %q", *section)
	}
	result.report(stdout, colorEnabled(stdout), *verbose)

	// A non-zero exit when anything fails makes the command usable in CI, and
	// keeps the number honest: there is no mode in which bindery claims a clean
	// run it did not have.
	if result.Passed != result.Total {
		return errSpecFailures{failed: result.Total - result.Passed, total: result.Total}
	}
	return nil
}

// errSpecFailures reports conformance shortfalls without the "bindery:" prefix
// an ordinary error would get, because the report above already said it.
type errSpecFailures struct{ failed, total int }

func (e errSpecFailures) Error() string {
	return fmt.Sprintf("%d of %d spec examples fail", e.failed, e.total)
}

func cmdRender(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("render", stderr)
	format := fs.String("format", "html", "output format: html, ansi, or json")
	if err := parse(fs, args); err != nil {
		return errUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "bindery render: expected exactly one file")
		return errUsage
	}

	// The format is validated before the file is read. A usage error should not
	// depend on whether an unrelated path happens to exist, and reporting one
	// without touching the disk is both faster and easier to reason about.
	switch *format {
	case "html", "json", "ansi":
	default:
		fmt.Fprintf(stderr, "bindery render: unknown format %q (want html, ansi or json)\n", *format)
		return errUsage
	}

	src, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	doc := Parse(string(src))

	switch *format {
	case "html":
		fmt.Fprint(stdout, RenderHTML(doc))
	case "json":
		// encoding/json/v2 with jsontext for formatting; both graduated out of
		// GOEXPERIMENT in Go 1.27.
		b, err := json.Marshal(doc.Root, jsontext.WithIndent("  "))
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s\n", b)
	case "ansi":
		fmt.Fprint(stdout, RenderANSI(doc, terminalWidth(), colorEnabled(stdout)))
	}
	return nil
}

// dirArg returns the positional directory argument, defaulting to the working
// directory.
func dirArg(fs *flag.FlagSet) string {
	if fs.NArg() > 0 {
		return filepath.Clean(fs.Arg(0))
	}
	return "."
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
