package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-graphviz"
)

const sample = "digraph G { a -> b; b -> c; a -> c }"

func TestParseArgs(t *testing.T) {
	cases := []struct {
		args []string
		want options
		err  bool
	}{
		{nil, options{layout: graphviz.DOT, format: graphviz.XDOT}, false},
		{[]string{"-Tpng", "-oout.png", "in.dot"}, options{layout: graphviz.DOT, format: graphviz.PNG, output: "out.png", input: "in.dot"}, false},
		{[]string{"-T", "svg", "-o", "out.svg", "-K", "neato", "-"}, options{layout: graphviz.NEATO, format: graphviz.SVG, output: "out.svg", input: "-"}, false},
		{[]string{"-h"}, options{layout: graphviz.DOT, format: graphviz.XDOT, help: true}, false},
		{[]string{"-V"}, options{layout: graphviz.DOT, format: graphviz.XDOT, version: true}, false},
		{[]string{"-Tgif"}, options{}, true},
		{[]string{"-Kbogus"}, options{}, true},
		{[]string{"-o"}, options{}, true},
		{[]string{"-x"}, options{}, true},
		{[]string{"a.dot", "b.dot"}, options{}, true},
	}
	for _, c := range cases {
		got, err := parseArgs(c.args)
		if (err != nil) != c.err {
			t.Errorf("%q: err=%v, want error=%v", c.args, err, c.err)
			continue
		}
		if !c.err && got != c.want {
			t.Errorf("%q: got %+v, want %+v", c.args, got, c.want)
		}
	}
}

func TestRenderFormats(t *testing.T) {
	magic := map[string]string{
		"dot": "digraph",
		"svg": "<?xml",
		"png": "\x89PNG",
		"jpg": "\xff\xd8\xff",
	}
	for format, want := range magic {
		var out, errb bytes.Buffer
		code := run([]string{"-T" + format}, strings.NewReader(sample), &out, &errb)
		if code != 0 {
			t.Fatalf("-T%s: exit %d: %s", format, code, errb.String())
		}
		if !strings.HasPrefix(out.String(), want) {
			t.Errorf("-T%s: output does not start with %q: %q", format, want, out.String()[:min(16, out.Len())])
		}
	}
}

func TestFileInAndOut(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.dot")
	out := filepath.Join(dir, "out.png")
	if err := os.WriteFile(in, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-Tpng", "-o" + out, in}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("wrote %d bytes to stdout with -o set", stdout.Len())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Errorf("output is not a PNG")
	}
}

func TestErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-Tgif"}, nil, &out, &errb); code != 2 {
		t.Errorf("bad flag: exit %d, want 2", code)
	}
	if code := run([]string{filepath.Join(t.TempDir(), "missing.dot")}, nil, &out, &errb); code != 1 {
		t.Errorf("missing input: exit %d, want 1", code)
	}
	if code := run(nil, strings.NewReader("digraph { a -> "), &out, &errb); code != 1 {
		t.Errorf("bad input: exit %d, want 1", code)
	}
	if code := run([]string{"-h"}, nil, &out, &errb); code != 0 || !strings.HasPrefix(out.String(), "usage:") {
		t.Errorf("-h: exit %d, out %q", code, out.String())
	}
}

func FuzzParseArgs(f *testing.F) {
	f.Add("-Tpng -oout.png in.dot")
	f.Add("-K neato -T svg -")
	f.Add("-h -V")
	f.Fuzz(func(t *testing.T, line string) {
		opt, err := parseArgs(strings.Fields(line))
		if err == nil && (opt.layout == "" || opt.format == "") {
			t.Errorf("%q: accepted with empty layout/format: %+v", line, opt)
		}
	})
}

func TestEmptyInputIsAnError(t *testing.T) {
	for _, in := range []string{"", "  \n"} {
		var out, errb bytes.Buffer
		if code := run([]string{"-Tsvg"}, strings.NewReader(in), &out, &errb); code != 1 {
			t.Errorf("%q: exit %d, want 1", in, code)
		}
		// A fresh engine parses empty input to a nil graph; one that has
		// parsed before reports a syntax error. Either is a parse error.
		if !strings.HasPrefix(errb.String(), "dot: parse input:") {
			t.Errorf("%q: stderr %q", in, errb.String())
		}
	}
}

// TestPprofGraph renders what `go tool pprof -dot` emits, the input the
// limen profile recipe hands to this command.
func TestPprofGraph(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "pprof.dot"))
	if err != nil {
		t.Fatal(err)
	}
	var svg, errb bytes.Buffer
	if code := run([]string{"-Tsvg"}, bytes.NewReader(src), &svg, &errb); code != 0 {
		t.Fatalf("svg: exit %d: %s", code, errb.String())
	}
	if n := strings.Count(svg.String(), `class="node"`); n < 10 {
		t.Errorf("svg has %d nodes, want at least 10", n)
	}
	var png bytes.Buffer
	if code := run([]string{"-Tpng"}, bytes.NewReader(src), &png, &errb); code != 0 {
		t.Fatalf("png: exit %d: %s", code, errb.String())
	}
	if !bytes.HasPrefix(png.Bytes(), []byte("\x89PNG")) {
		t.Errorf("png output is not a PNG")
	}
}
