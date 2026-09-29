// Command dot renders a Graphviz graph to dot, svg, png or jpg without a
// native graphviz installation: the engine is go-graphviz's WASM build.
//
// Usage:
//
//	dot [-K layout] [-T format] [-o output] [input]
//
// The input is a file in the DOT language, or standard input when absent or
// "-". Without -o the rendering goes to standard output. Flags accept both the
// glued form graphviz users type (-Tpng, -oout.png) and the spaced one.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/goccy/go-graphviz"
)

const usage = `usage: dot [-K layout] [-T format] [-o output] [input]

  -K layout   layout engine: dot (default), neato, fdp, sfdp, circo, twopi,
              osage, patchwork
  -T format   output format: dot (default), svg, png, jpg
  -o output   output file (default: standard output)
  -V          print the version
  -h          print this help
  input       DOT file (default, or "-": standard input)
`

// options is the parsed command line.
type options struct {
	layout  graphviz.Layout
	format  graphviz.Format
	output  string
	input   string
	version bool
	help    bool
}

var (
	errUsage = errors.New("usage")

	// errNoGraph is empty or whitespace-only input, which parses to a nil
	// graph without an error.
	errNoGraph = errors.New("no graph found")

	layouts = map[string]graphviz.Layout{
		"dot": graphviz.DOT, "neato": graphviz.NEATO, "fdp": graphviz.FDP,
		"sfdp": graphviz.SFDP, "circo": graphviz.CIRCO, "twopi": graphviz.TWOPI,
		"osage": graphviz.OSAGE, "patchwork": graphviz.PATCHWORK,
	}
	formats = map[string]graphviz.Format{
		"dot": graphviz.XDOT, "svg": graphviz.SVG, "png": graphviz.PNG, "jpg": graphviz.JPG,
	}
)

// parseArgs accepts graphviz's glued flags (-Tpng) as well as spaced ones,
// which the standard flag package cannot do.
func parseArgs(args []string) (options, error) {
	opt := options{layout: graphviz.DOT, format: graphviz.XDOT}

	take := func(index *int, glued string) (string, error) {
		if glued != "" {
			return glued, nil
		}

		*index++
		if *index >= len(args) {
			return "", fmt.Errorf("%w: %s needs a value", errUsage, args[*index-1])
		}

		return args[*index], nil
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-h", arg == "--help":
			opt.help = true
		case arg == "-V", arg == "--version":
			opt.version = true
		case arg == "-", !strings.HasPrefix(arg, "-"):
			if opt.input != "" {
				return opt, fmt.Errorf("%w: one input at most, got %q and %q", errUsage, opt.input, arg)
			}

			opt.input = arg
		case strings.HasPrefix(arg, "-K"):
			value, err := take(&index, arg[2:])
			if err != nil {
				return opt, err
			}

			l, ok := layouts[value]
			if !ok {
				return opt, fmt.Errorf("%w: unknown layout %q", errUsage, value)
			}

			opt.layout = l
		case strings.HasPrefix(arg, "-T"):
			value, err := take(&index, arg[2:])
			if err != nil {
				return opt, err
			}

			f, ok := formats[value]
			if !ok {
				return opt, fmt.Errorf("%w: unknown format %q", errUsage, value)
			}

			opt.format = f
		case strings.HasPrefix(arg, "-o"):
			value, err := take(&index, arg[2:])
			if err != nil {
				return opt, err
			}

			opt.output = value
		default:
			return opt, fmt.Errorf("%w: unknown flag %q", errUsage, arg)
		}
	}

	return opt, nil
}

func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}

	return "(devel)"
}

// graphvizMu serializes every call into go-graphviz. v0.2.10 runs them all
// through one WASM instance per process, set up in a package init, and does
// not serialize them itself: two renders at once read and write the same
// guest memory.
//
//nolint:gochecknoglobals // it guards a process-wide WASM instance, so it is process-wide too
var graphvizMu sync.Mutex

// render reads DOT from in, lays it out and writes format to out.
func render(ctx context.Context, opt options, in io.Reader, out io.Writer) error {
	src, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}

	// Held from the parse, which already runs in the WASM instance, until the
	// engine is closed: the deferred Close below runs before this Unlock.
	graphvizMu.Lock()
	defer graphvizMu.Unlock()

	graph, err := graphviz.ParseBytes(src)
	if err != nil {
		return fmt.Errorf("parse input: %w", err)
	}
	// Empty or whitespace-only input parses to a nil graph without an error,
	// and rendering nil faults inside the WASM engine.
	if graph == nil {
		return fmt.Errorf("parse input: %w", errNoGraph)
	}

	engine, err := graphviz.New(ctx)
	if err != nil {
		return fmt.Errorf("start graphviz: %w", err)
	}

	defer func() { _ = engine.Close() }()

	engine.SetLayout(opt.layout)

	if err := engine.Render(ctx, graph, opt.format, out); err != nil {
		return fmt.Errorf("render: %w", err)
	}

	return nil
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opt, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "dot:", err)
		fmt.Fprint(stderr, usage)

		return 2
	}

	switch {
	case opt.help:
		fmt.Fprint(stdout, usage)
		return 0
	case opt.version:
		fmt.Fprintln(stdout, version())
		return 0
	}

	input := stdin

	if opt.input != "" && opt.input != "-" {
		file, err := os.Open(opt.input)
		if err != nil {
			fmt.Fprintln(stderr, "dot:", err)
			return 1
		}
		defer func() { _ = file.Close() }()

		input = file
	}

	if opt.output == "" {
		if err := render(context.Background(), opt, input, stdout); err != nil {
			fmt.Fprintln(stderr, "dot:", err)
			return 1
		}

		return 0
	}

	if err := renderToFile(context.Background(), opt, input); err != nil {
		fmt.Fprintln(stderr, "dot:", err)
		return 1
	}

	return 0
}

// renderToFile renders into a temporary file beside opt.output and renames
// it onto the path only once the render has completed: a failed render, an
// interrupt or a full disk leaves whatever was at the path untouched rather
// than a truncated file that a later build would take for a finished one.
func renderToFile(ctx context.Context, opt options, input io.Reader) (err error) {
	dir, base := filepath.Split(opt.output)

	tmp, err := os.CreateTemp(dir, "."+base+".*")
	if err != nil {
		//nolint:wrapcheck // os's *PathError already names the operation and the path
		return err
	}

	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if err = render(ctx, opt, input, tmp); err != nil {
		return err
	}

	if err = tmp.Close(); err != nil {
		//nolint:wrapcheck // os's *PathError already names the operation and the path
		return err
	}

	//nolint:wrapcheck // os's *LinkError already names the operation and both paths
	return os.Rename(tmp.Name(), opt.output)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
