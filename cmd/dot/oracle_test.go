package main

// What the command hands the engine, held two ways. In process, the engine
// is its own oracle: the output under -K and -T is byte for byte what
// go-graphviz renders with that layout and format, so a flag that did not
// reach the engine is seen. Across builds, Graphviz's own dot is the oracle
// for the language: what the command writes under -Tdot and -Txdot is DOT
// the native dot parses, and what the native dot writes is DOT the command
// renders. The layout itself is not compared: the two builds of the same
// Graphviz differ on text metrics and on the force-directed engines, and
// the engine is go-graphviz's to hold. The native half is local only: it
// skips unless DOT_ORACLE names a Graphviz dot whose major.minor is the
// engine's, read from go-graphviz's pins.yaml at the module built with.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

var layouts = map[string]graphviz.Layout{ //nolint:gochecknoglobals // immutable data
	"dot": graphviz.DOT, "neato": graphviz.NEATO, "fdp": graphviz.FDP, "sfdp": graphviz.SFDP,
	"circo": graphviz.CIRCO, "twopi": graphviz.TWOPI, "osage": graphviz.OSAGE, "patchwork": graphviz.PATCHWORK,
}

var formats = map[string]graphviz.Format{ //nolint:gochecknoglobals // immutable data
	"dot": graphviz.GV, "xdot": graphviz.XDOT, "svg": graphviz.SVG, "png": graphviz.PNG, "jpg": graphviz.JPG,
}

var graphs = map[string]string{ //nolint:gochecknoglobals // immutable data
	"chain":  "digraph { a -> b -> c }",
	"fan":    "digraph { a -> {b c d} }",
	"plain":  "graph { a -- b; b -- c; c -- a }",
	"single": "digraph { only }",
}

// engine renders src with the library directly, as the command should.
func engine(t *testing.T, src string, layout graphviz.Layout, format graphviz.Format) []byte {
	t.Helper()

	ctx := t.Context()

	graph, err := graphviz.ParseBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = g.Close() }()

	g.SetLayout(layout)

	var out bytes.Buffer
	if err := g.Render(ctx, graph, format, &out); err != nil {
		t.Fatal(err)
	}

	return out.Bytes()
}

func TestFlagsReachTheEngine(t *testing.T) {
	t.Parallel()

	for layoutName, layout := range layouts {
		for graphName, src := range graphs {
			var stdout, stderr bytes.Buffer

			if code := run([]string{"-K" + layoutName, "-Tdot"}, strings.NewReader(src), &stdout, &stderr); code != 0 {
				t.Fatalf("-K%s %s: exit %d: %s", layoutName, graphName, code, stderr.String())
			}

			if want := engine(t, src, layout, graphviz.GV); !bytes.Equal(stdout.Bytes(), want) {
				t.Errorf("-K%s %s: the command's output is not the engine's for that layout", layoutName, graphName)
			}
		}
	}

	// No -K is dot; the raster formats are compared by header, their
	// encoders carrying no layout.
	var plain, explicit bytes.Buffer

	run(nil, strings.NewReader(graphs["fan"]), &plain, &bytes.Buffer{})
	run([]string{"-Kdot"}, strings.NewReader(graphs["fan"]), &explicit, &bytes.Buffer{})

	if !bytes.Equal(plain.Bytes(), explicit.Bytes()) {
		t.Error("no -K is not -Kdot")
	}

	for formatName, format := range formats {
		var stdout, stderr bytes.Buffer

		if code := run([]string{"-T" + formatName}, strings.NewReader(graphs["fan"]), &stdout, &stderr); code != 0 {
			t.Fatalf("-T%s: exit %d: %s", formatName, code, stderr.String())
		}

		got, want := stdout.Bytes(), engine(t, graphs["fan"], graphviz.DOT, format)
		if formatName == "png" || formatName == "jpg" {
			got, want = got[:8], want[:8]
		}

		if !bytes.Equal(got, want) {
			t.Errorf("-T%s: the command's output is not the engine's for that format", formatName)
		}
	}
}

// enginePin is the Graphviz release the engine embeds, from go-graphviz's
// pins.yaml at the module the command builds with.
func enginePin(t *testing.T) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Dir}}", "github.com/forkcloser/go-graphviz").
		Output()
	if err != nil {
		t.Skipf("the engine module's directory: %v", err)
	}

	pins, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "pins.yaml"))
	if err != nil {
		t.Skipf("the engine's pins: %v", err)
	}

	match := regexp.MustCompile(`(?s)- name: graphviz\n.*?\n\s+version: (\S+)`).FindSubmatch(pins)
	if match == nil {
		t.Skip("no graphviz pin in the engine's pins.yaml")
	}

	return string(match[1])
}

func majorMinor(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return version
	}

	return parts[0] + "." + parts[1]
}

// nativeDot is the Graphviz dot the oracle runs, or a skip.
func nativeDot(t *testing.T) string {
	t.Helper()

	path := os.Getenv("DOT_ORACLE")
	if path == "" {
		t.Skip("set DOT_ORACLE to a Graphviz dot to hold the command's DOT to it")
	}

	out, err := exec.CommandContext(t.Context(), path, "-V").CombinedOutput()
	if err != nil {
		t.Skipf("%s -V: %v", path, err)
	}

	// "dot - graphviz version 16.1.0 (…)"
	fields := strings.Fields(string(out))
	version := ""

	for i, field := range fields {
		if field == "version" && i+1 < len(fields) {
			version = fields[i+1]
		}
	}

	if pin := enginePin(t); majorMinor(version) != majorMinor(pin) {
		t.Skipf("the native dot is %s, the engine %s", version, pin)
	}

	return path
}

// native runs the Graphviz dot on src and returns its output or its error.
func native(t *testing.T, path, src string, args ...string) ([]byte, error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), path, args...)
	cmd.Stdin = strings.NewReader(src)

	return cmd.Output()
}

func TestDOTIsGraphvizsDOTBothWays(t *testing.T) {
	t.Parallel()

	nativePath := nativeDot(t)
	dir := t.TempDir()

	for layoutName := range layouts {
		for graphName, src := range graphs {
			for _, format := range []string{"dot", "xdot"} {
				ours := dot(t, dir, src, "-K"+layoutName, "-T"+format)
				if ours.code != 0 {
					t.Fatalf("-K%s -T%s %s: exit %d: %s", layoutName, format, graphName, ours.code, ours.stderr)
				}

				// The native dot parses what the command wrote, and keeps
				// every node of it.
				canon, err := native(t, nativePath, ours.stdout, "-Tcanon")
				if err != nil {
					t.Errorf(
						"-K%s -T%s %s: the native dot refuses the command's output: %v",
						layoutName,
						format,
						graphName,
						err,
					)

					continue
				}

				if got, want := nodeNames(t, string(canon)), nodeNames(t, src); got != want {
					t.Errorf(
						"-K%s -T%s %s: nodes %q after the round trip, want %q",
						layoutName,
						format,
						graphName,
						got,
						want,
					)
				}
			}
		}
	}

	// The command renders what the native dot wrote, layout included.
	for graphName, src := range graphs {
		theirs, err := native(t, nativePath, src, "-Tdot")
		if err != nil {
			t.Fatal(err)
		}

		ours := dot(t, dir, string(theirs), "-Tsvg")
		if ours.code != 0 || !strings.HasPrefix(ours.stdout, "<?xml") {
			t.Errorf("%s: the command refuses the native dot's output: exit %d: %s", graphName, ours.code, ours.stderr)
		}
	}
}

// nodeNames is the sorted node names of a DOT text, read by the engine's
// own parser.
func nodeNames(t *testing.T, src string) string {
	t.Helper()

	graph, err := graphviz.ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("parsing %q: %v", src, err)
	}

	defer func() { _ = graph.Close() }()

	var names []string

	for node, err := graph.FirstNode(); node != nil && err == nil; node, err = graph.NextNode(node) {
		name, nameErr := node.Name()
		if nameErr != nil {
			t.Fatal(nameErr)
		}

		names = append(names, name)
	}

	slices.Sort(names)

	return strings.Join(names, " ")
}
