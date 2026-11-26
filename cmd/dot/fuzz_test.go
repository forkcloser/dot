package main

// Past the fixed cases: any bytes as the input, under every format. The
// command never panics or faults in the engine; it exits 0 with a rendering
// on stdout and nothing on stderr, or 1 with "dot: " on stderr. An exit 0
// with nothing on stdout is a text with no graph in it, and only that. An
// exit 1 has a rendering on stdout only when the text begins with a graph:
// one drawn before a part that failed, or one Graphviz reported an error
// about while it drew on. Under -o the path holds a rendering when stdout
// had one, and is as it was when it had none.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/cgraph"
)

func FuzzInput(f *testing.F) {
	f.Add(graph, uint8(0))
	f.Add("graph { a -- b; b -- c }", uint8(1))
	f.Add("digraph { a -> b [label=\"x\"]; subgraph cluster_0 { c; d } }", uint8(2))
	f.Add("", uint8(3))
	f.Add("digraph { a -> ", uint8(4))
	f.Add("strict digraph { a -> a }", uint8(0))
	f.Add("digraph { a [shape=record, label=\"{a|b}\"] }", uint8(1))
	f.Add("digraph { \"a\\\"b\" -> c }", uint8(2))
	f.Add("digraph { a -> b } digraph { c -> d }", uint8(3))
	f.Add("digraph { node [fontsize=0]; a }", uint8(4))
	f.Add("digraph { a [width=1e9] }", uint8(2))
	f.Add("/* comment */ digraph { a }", uint8(0))
	f.Add("digraph { a }\ndigraph { b -> }\ndigraph { c }", uint8(0))
	f.Add("digraph { a } nonsense", uint8(2))
	f.Add("digraph { a [shape=record, label=\"{a|\"] } graph { b }", uint8(1))

	formats := []string{"dot", "xdot", "svg", "png", "jpg"}

	f.Fuzz(func(t *testing.T, input string, pick uint8) {
		format := formats[int(pick)%len(formats)]

		var stdout, stderr bytes.Buffer

		code := run([]string{"-T" + format}, strings.NewReader(input), &stdout, &stderr)

		switch code {
		case 0:
			if stderr.Len() != 0 {
				t.Fatalf("exit 0 with stderr %q", stderr.String())
			}

			parsed, err := graphviz.ParseBytes([]byte(input))
			if parsed != nil {
				_ = parsed.Close()
			}

			if (stdout.Len() == 0) != errors.Is(err, cgraph.ErrNoGraph) {
				t.Fatalf("exit 0 with %d bytes out for a text that parses to %v", stdout.Len(), err)
			}
		case 1:
			if !strings.HasPrefix(stderr.String(), "dot: ") {
				t.Fatalf("exit 1 with stderr %q", stderr.String())
			}

			if stdout.Len() != 0 {
				parsed, err := graphviz.ParseBytes([]byte(input))
				if err != nil {
					t.Fatalf(
						"exit 1 with %d bytes out for a text whose first graph is %v: %q",
						stdout.Len(),
						err,
						stderr.String(),
					)
				}

				_ = parsed.Close()
			}
		default:
			t.Fatalf("exit %d for an input: %q", code, stderr.String())
		}

		// Under -o: the same verdict, the file written when stdout had a
		// rendering and untouched when it had none.
		dir := t.TempDir()
		out := filepath.Join(dir, "out")

		if err := os.WriteFile(out, []byte("previous"), 0o600); err != nil {
			t.Fatal(err)
		}

		var stdout2, stderr2 bytes.Buffer

		code2 := run([]string{"-T" + format, "-o", out}, strings.NewReader(input), &stdout2, &stderr2)
		if code2 != code || stdout2.Len() != 0 {
			t.Fatalf("under -o: exit %d (was %d), stdout %q", code2, code, stdout2.String())
		}

		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}

		// The file holds a rendering of the same format. Not the same
		// bytes: the engine's first render in a process writes the DOT
		// without the node default line every later render carries, held
		// as a seed until the engine is fixed.
		rendered := stdout.Len() != 0

		if rendered &&
			(len(data) == 0 || !bytes.Equal(data[:min(8, len(data))], stdout.Bytes()[:min(8, stdout.Len())])) {
			t.Fatalf(
				"the file is not a rendering like stdout's: %q against %q",
				firstBytes(data),
				firstBytes(stdout.Bytes()),
			)
		}

		if !rendered && string(data) != "previous" {
			t.Fatalf("a failed render touched the output: %q", firstBytes(data))
		}

		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("%d entries beside the output: %v", len(entries), err)
		}
	})
}
