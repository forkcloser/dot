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
	"runtime/debug"
	"strings"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
	"github.com/forkcloser/xos"
)

const usage = `usage: dot [-K engine] [-T format] [-o path] [input]

  -K engine   layout engine: dot (default), neato, fdp, sfdp, circo, twopi,
              osage, patchwork
  -T format   output format: dot (default), xdot, svg, png, jpg
  -o path     output file (default: standard output)
  -V          print the version, on standard error
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

// version is the line -V prints, in the shape of Graphviz's dot's: the
// Graphviz release the engine embeds where Graphviz's dot names its own, and
// this command's version where that one puts its build date.
func version() (string, error) {
	engine, err := graphviz.Version()
	if err != nil {
		return "", fmt.Errorf("the engine's version: %w", err)
	}

	own := "devel"
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		own = bi.Main.Version
	}

	return fmt.Sprintf("dot - graphviz version %s (forkcloser/dot %s)", engine, own), nil
}

// draw renders every graph of src to out, one after the other as the text
// has them, and returns whether out got a rendering and the status to exit
// with.
//
// As with Graphviz's dot, a graph that fails to lay out or render, or that
// Graphviz reports an error about while it draws it, does not stop the
// graphs after it; a part of the text that does not parse does, since
// nothing can be read past it. Either makes the status a failure, with what
// was rendered before written all the same.
//
// A write to out that fails ends the drawing too, and out then counts as
// having got nothing: the error is out's and not a graph's, and no graph
// after it can make whole what out holds.
func draw(ctx context.Context, opt options, src []byte, out, stderr io.Writer) (wrote bool, status int) {
	var engine *graphviz.Graphviz

	sink := &recorder{to: out}

	defer func() {
		if engine != nil {
			_ = engine.Close()
		}
	}()

	for graph, err := range graphviz.ParseAll(src) {
		if err != nil {
			fmt.Fprintln(stderr, "dot: parse input:", err)

			return wrote, statusFailed
		}

		if engine, err = started(ctx, engine, opt); err != nil {
			_ = graph.Close()

			fmt.Fprintln(stderr, "dot: start graphviz:", err)

			return wrote, statusFailed
		}

		err = engine.Render(ctx, graph, opt.format, sink)
		_ = graph.Close()

		if sink.err != nil {
			fmt.Fprintln(stderr, "dot:", sink.err)

			return false, statusFailed
		}

		// An error that is gvc.ErrReported came after the whole rendering.
		if err == nil || errors.Is(err, gvc.ErrReported) {
			wrote = true
		}

		if err != nil {
			fmt.Fprintln(stderr, "dot: render:", err)

			status = statusFailed
		}
	}

	return wrote, status
}

// started is engine, or, when there is none yet, a new one that lays out
// as opt asks: an input with nothing to draw starts none.
func started(ctx context.Context, engine *graphviz.Graphviz, opt options) (*graphviz.Graphviz, error) {
	if engine != nil {
		return engine, nil
	}

	engine, err := graphviz.New(ctx)
	if err != nil {
		//nolint:wrapcheck // draw says what failed
		return nil, err
	}

	engine.SetLayout(opt.layout)

	return engine, nil
}

// recorder is a writer that keeps the first error of the one behind it.
type recorder struct {
	to  io.Writer
	err error
}

func (r *recorder) Write(p []byte) (int, error) {
	n, err := r.to.Write(p)
	if err != nil && r.err == nil {
		r.err = err
	}

	//nolint:wrapcheck // the writer's own error, which draw reports as it is
	return n, err
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
		var line string

		if line, err = version(); err != nil {
			fmt.Fprintln(stderr, "dot:", err)
			return statusFailed
		}

		// On standard error, where Graphviz's dot prints its own.
		fmt.Fprintln(stderr, line)

		return 0
	}

	src, err := read(opt, stdin)
	if err != nil {
		fmt.Fprintln(stderr, "dot:", err)
		return statusNoInput
	}

	if opt.output == "" {
		_, status := draw(context.Background(), opt, src, stdout, stderr)

		return status
	}

	status, err := drawToFile(context.Background(), opt, src, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "dot:", err)

		return statusFailed
	}

	return status
}

// drawToFile draws into the file opt.output names, and returns the status
// of the drawing and the error of the file, the graphs' own being on
// stderr. The path gets every rendering standard output would have got;
// when there is none, no file is made and the one at the path stays as it
// was.
func drawToFile(ctx context.Context, opt options, src []byte, stderr io.Writer) (int, error) {
	out := &output{path: opt.output}

	// Whatever is not finished is given up: after a write that failed,
	// what the temporary file holds is not to reach the path.
	defer out.discard()

	wrote, status := draw(ctx, opt, src, out, stderr)
	if !wrote {
		return status, nil
	}

	if err := out.finish(); err != nil {
		return statusFailed, err
	}

	return status, nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
