// Command dot renders a Graphviz graph to dot, xdot, svg, png or jpg without a
// native graphviz installation: the engine is go-graphviz's WASM build.
//
// Usage:
//
//	dot [-K layout] [-T format] [-o output] [input]
//
// The input is a file in the DOT language, or standard input when absent or
// "-". Without -o the rendering goes to standard output. Flags accept both the
// glued form graphviz users type (-Tpng, -oout.png) and the spaced one.
//
// The exit statuses are those of Graphviz's dot: 0 on success, 2 for an input
// that cannot be read, 1 for every other failure.
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

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/cgraph"
	"github.com/forkcloser/go-graphviz/gvc"
	"github.com/forkcloser/xos"
)

const usage = `usage: dot [-K engine] [-T format] [-o path] [input]

  -K engine   layout engine: dot (default), neato, fdp, sfdp, circo, twopi,
              osage, patchwork
  -T format   output format: dot (default), xdot, svg, png, jpg
  -o path     output file (default: standard output)
  -V          print the version
  -h, -?      print this help
  input       DOT file (default, or "-": standard input)
`

// options is the parsed command line.
type options struct {
	layout graphviz.Layout
	format graphviz.Format
	output string
	input  string
	// named says an input was given: an empty name is one, a file that
	// cannot be opened, and not standard input.
	named   bool
	version bool
	help    bool
}

var errUsage = errors.New("usage")

// The statuses a failure exits with, as Graphviz's dot has them: an input it
// cannot read is told apart from every other failure, a command line it
// cannot take included.
const (
	statusFailed  = 1
	statusNoInput = 2
)

// layoutNamed is the layout engine graphviz knows by name.
func layoutNamed(name string) (graphviz.Layout, bool) {
	switch name {
	case "dot":
		return graphviz.DOT, true
	case "neato":
		return graphviz.NEATO, true
	case "fdp":
		return graphviz.FDP, true
	case "sfdp":
		return graphviz.SFDP, true
	case "circo":
		return graphviz.CIRCO, true
	case "twopi":
		return graphviz.TWOPI, true
	case "osage":
		return graphviz.OSAGE, true
	case "patchwork":
		return graphviz.PATCHWORK, true
	default:
		return "", false
	}
}

// formatNamed is the output format by the name -T takes, as Graphviz names
// them: "dot" is the layout written back as DOT with the computed positions
// on it, "xdot" that plus the drawing operations.
func formatNamed(name string) (graphviz.Format, bool) {
	switch name {
	case "dot":
		return graphviz.GV, true
	case "xdot":
		return graphviz.XDOT, true
	case "svg":
		return graphviz.SVG, true
	case "png":
		return graphviz.PNG, true
	case "jpg":
		return graphviz.JPG, true
	default:
		return "", false
	}
}

// parseArgs accepts graphviz's glued flags (-Tpng) as well as spaced ones,
// which the standard flag package cannot do.
func parseArgs(args []string) (options, error) {
	opt := options{layout: graphviz.DOT, format: graphviz.GV}

	// A spaced value flag consumes the argument after it, so the loop moves
	// the index itself rather than ranging over the slice.
	index := 0
	for index < len(args) {
		arg := args[index]

		switch {
		// As with Graphviz's dot, the first of these to be reached is the
		// whole command: what follows it is not read, a bad flag included.
		case arg == "-h", arg == "-?", arg == "--help":
			opt.help = true
			return opt, nil
		case arg == "-V", arg == "--version":
			opt.version = true
			return opt, nil
		case arg == "-", !strings.HasPrefix(arg, "-"):
			if opt.named {
				return opt, fmt.Errorf("%w: one input at most, got %q and %q", errUsage, opt.input, arg)
			}

			opt.input, opt.named = arg, true
		default:
			var err error
			if index, err = parseFlag(&opt, args, index); err != nil {
				return opt, err
			}
		}

		index++
	}

	return opt, nil
}

// parseFlag reads the value flag at args[index] into opt, its value glued on
// (-Tpng) or the next argument (-T png), and returns the index of the last
// argument it consumed.
func parseFlag(opt *options, args []string, index int) (int, error) {
	arg := args[index]

	flag, value := arg[:2], arg[2:]
	if flag != "-K" && flag != "-T" && flag != "-o" {
		return index, fmt.Errorf("%w: unknown flag %q", errUsage, arg)
	}

	if value == "" {
		index++
		if index >= len(args) {
			return index, fmt.Errorf("%w: %s needs a value", errUsage, arg)
		}

		value = args[index]
	}

	switch flag {
	case "-K":
		layout, ok := layoutNamed(value)
		if !ok {
			return index, fmt.Errorf("%w: unknown layout %q", errUsage, value)
		}

		opt.layout = layout
	case "-T":
		format, ok := formatNamed(value)
		if !ok {
			return index, fmt.Errorf("%w: unknown format %q", errUsage, value)
		}

		opt.format = format
	default:
		opt.output = value
	}

	return index, nil
}

func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}

	return "(devel)"
}

// render lays graph out and writes it to out in the format asked for.
func render(ctx context.Context, opt options, graph *graphviz.Graph, out io.Writer) error {
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

// read is the DOT text of the input: the file named, or standard input.
func read(opt options, stdin io.Reader) ([]byte, error) {
	if opt.named && opt.input != "-" {
		// Read whole and closed here, not held open through the render:
		// with -o naming the input, the rendering is renamed over it.
		//nolint:wrapcheck // os's *PathError already names the operation and the path
		return xos.ReadFile(opt.input)
	}

	src, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}

	return src, nil
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opt, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "dot:", err)
		fmt.Fprint(stderr, usage)

		return statusFailed
	}

	switch {
	case opt.help:
		fmt.Fprint(stdout, usage)
		return 0
	case opt.version:
		fmt.Fprintln(stdout, version())
		return 0
	}

	src, err := read(opt, stdin)
	if err != nil {
		fmt.Fprintln(stderr, "dot:", err)
		return statusNoInput
	}

	graph, err := graphviz.ParseBytes(src)

	switch {
	case errors.Is(err, cgraph.ErrNoGraph):
		// A text with no graph in it, empty or comments alone, is nothing to
		// draw: as Graphviz's dot, nothing is written, no file is made or
		// replaced, and that is no failure.
		return 0
	case err != nil:
		fmt.Fprintln(stderr, "dot: parse input:", err)
		return statusFailed
	}

	defer func() { _ = graph.Close() }()

	if opt.output == "" {
		err = render(context.Background(), opt, graph, stdout)
	} else {
		err = renderToFile(context.Background(), opt, graph)
	}

	if err != nil {
		fmt.Fprintln(stderr, "dot:", err)
		return statusFailed
	}

	return 0
}

// renderToFile renders into a temporary file beside opt.output and renames
// it onto the path only once the render has completed: a failed render, an
// interrupt or a full disk leaves whatever was at the path untouched rather
// than a truncated file that a later build would take for a finished one.
func renderToFile(ctx context.Context, opt options, graph *graphviz.Graph) error {
	dir, base := filepath.Split(opt.output)
	if dir == "" {
		// A bare name: beside it is the working directory, not the
		// system's temporary directory CreateTemp takes "" for, which
		// may be another filesystem the rename cannot cross.
		dir = "."
	}

	tmp, err := xos.CreateTemp(dir, "."+base+".*")
	if err != nil {
		//nolint:wrapcheck // os's *PathError already names the operation and the path
		return err
	}

	placed := false

	defer func() {
		if !placed {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	// An error that is gvc.ErrReported came after the whole rendering: the
	// file goes to its path all the same, and the error to the exit status.
	reported := render(ctx, opt, graph, tmp)
	if reported != nil && !errors.Is(reported, gvc.ErrReported) {
		return reported
	}

	if err := tmp.Close(); err != nil {
		//nolint:wrapcheck // os's *PathError already names the operation and the path
		return err
	}

	if err := xos.Rename(tmp.Name(), opt.output); err != nil {
		//nolint:wrapcheck // os's *LinkError already names the operation and both paths
		return err
	}

	placed = true

	return reported
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
