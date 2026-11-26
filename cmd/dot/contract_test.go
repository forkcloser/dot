package main

// The command's contract at the process boundary, from the README: the
// flags in their glued and spaced forms, -V and -h, the input as a file,
// "-" or standard input, -o as a path in every shape, and the exit
// statuses 0, 1 and 2 with the usage on stderr for a command line the
// command cannot take. Each case runs the built binary in a child.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A child that outlives its test binary is not killed with it: every one
// gets a deadline of its own.
const (
	buildDeadline = 5 * time.Minute
	childDeadline = time.Minute
)

// binary is the command built once for the package's tests.
var binary string //nolint:gochecknoglobals // built by TestMain, read by every case

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "dot-test")
	if err != nil {
		panic(err)
	}

	binary = filepath.Join(dir, "dot"+exeSuffix())

	ctx, cancel := context.WithTimeout(context.Background(), buildDeadline)

	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	build.WaitDelay = time.Second

	out, buildErr := build.CombinedOutput()

	cancel()

	if buildErr != nil {
		panic(string(out))
	}

	// The runner exits with m.Run's result once TestMain returns.
	m.Run()

	_ = os.RemoveAll(dir)
}

// exeSuffix is what the OS needs appended to run a binary built to a bare name.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}

	return ""
}

// result is what a child run leaves.
type result struct {
	code           int
	stdout, stderr string
}

// child is a command that is killed at its deadline, or when its test ends.
func child(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), childDeadline)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second

	return cmd
}

func dot(t *testing.T, dir, stdin string, args ...string) result {
	t.Helper()

	cmd := child(t, binary, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)

	var stdout, stderr bytes.Buffer

	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()

	code := 0

	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running %q: %v", args, err)
	}

	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

const graph = "digraph G { a -> b }\n"

func TestExitStatusesAndStreams(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	in := filepath.Join(dir, "in.dot")

	if err := os.WriteFile(in, []byte(graph), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		stdin  string
		args   []string
		code   int
		stdout string // a prefix, when set
		usage  bool   // the usage on stderr
	}{
		{"a graph on stdin", graph, nil, 0, "digraph", false},
		{"a graph by file", "", []string{in}, 0, "digraph", false},
		{"a graph by dash", graph, []string{"-"}, 0, "digraph", false},
		{"glued format", graph, []string{"-Tsvg"}, 0, "<?xml", false},
		{"spaced format", graph, []string{"-T", "svg"}, 0, "<?xml", false},
		{"glued layout", graph, []string{"-Kneato"}, 0, "digraph", false},
		{"spaced layout", graph, []string{"-K", "neato"}, 0, "digraph", false},
		{"flags after the input", "", []string{in, "-Tsvg"}, 0, "<?xml", false},
		{"-h", "", []string{"-h"}, 0, "usage:", false},
		{"--help", "", []string{"--help"}, 0, "usage:", false},
		{"-V", "", []string{"-V"}, 0, "", false},
		{"--version", "", []string{"--version"}, 0, "", false},
		{"-h beats a bad graph", "nonsense", []string{"-h"}, 0, "usage:", false},
		{"-V beats a bad graph", "nonsense", []string{"-V"}, 0, "", false},
		{"-?", "", []string{"-?"}, 0, "usage:", false},
		{"-h before -V", "", []string{"-h", "-V"}, 0, "usage:", false},
		{"-? before a bad flag", "", []string{"-?", "-Tgif"}, 0, "usage:", false},
		{"-V before a bad flag", "", []string{"-V", "-Tgif"}, 0, "", false},
		{"-V before -h", "", []string{"-V", "-h"}, 0, "", false},
		{"a bad flag before -?", "", []string{"-Tgif", "-?"}, 1, "", true},
		{"unknown flag", graph, []string{"-x"}, 1, "", true},
		{"unknown format", graph, []string{"-Tgif"}, 1, "", true},
		{"unknown layout", graph, []string{"-Kbogus"}, 1, "", true},
		{"a flag without its value", graph, []string{"-o"}, 1, "", true},
		{"two inputs", graph, []string{in, in}, 1, "", true},
		{"a long flag that is not help or version", graph, []string{"--format=svg"}, 1, "", true},
		{"usage error beats a missing file", "", []string{"-Tgif", filepath.Join(dir, "missing")}, 1, "", true},
		{"a missing file", "", []string{filepath.Join(dir, "missing.dot")}, 2, "", false},
		{"a directory as input", "", []string{dir}, 2, "", false},
		{"a syntax error", "digraph { a -> ", nil, 1, "", false},
		{"an undirected edge in a digraph", "digraph { a -- b }", nil, 1, "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := dot(t, dir, c.stdin, c.args...)
			if got.code != c.code {
				t.Fatalf("exit %d, want %d\nstdout %q\nstderr %q", got.code, c.code, got.stdout, got.stderr)
			}

			if c.stdout != "" && !strings.HasPrefix(got.stdout, c.stdout) {
				t.Fatalf("stdout %q, want a prefix %q", got.stdout, c.stdout)
			}

			switch {
			case c.code == 0 && c.stdout == "":
				// -V: its one line is on standard error, where Graphviz's
				// dot prints its own, and standard output has nothing.
				if got.stdout != "" || !versionLine.MatchString(got.stderr) {
					t.Fatalf("stdout %q, stderr %q; want nothing and the version line", got.stdout, got.stderr)
				}
			case c.code == 0:
				if got.stderr != "" {
					t.Fatalf("stderr on success: %q", got.stderr)
				}
			default:
				if got.stdout != "" {
					t.Fatalf("stdout on failure: %q", got.stdout)
				}

				if !strings.HasPrefix(got.stderr, "dot: ") {
					t.Fatalf("stderr does not name the command: %q", got.stderr)
				}

				if c.usage != strings.Contains(got.stderr, "usage: dot") {
					t.Fatalf("usage on stderr = %v, want %v:\n%s", !c.usage, c.usage, got.stderr)
				}
			}
		})
	}
}

// The statuses of a failure are those of Graphviz's dot: 2 for an input that
// cannot be read, 1 for a command line it cannot take. Each case is held to
// the native dot when there is one; a directory as the input, where the
// command differs from it on purpose, is in the table above alone.
func TestFailuresExitAsGraphvizsDot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "in.dot"), []byte(graph), 0o600); err != nil {
		t.Fatal(err)
	}

	// A file that is there and may not be opened. Where the mode does not
	// refuse the open (Windows, a run as root), the case is skipped.
	if err := os.WriteFile(filepath.Join(dir, "closed.dot"), []byte(graph), 0o000); err != nil {
		t.Fatal(err)
	}

	closed, err := os.Open(filepath.Join(dir, "closed.dot"))
	if err == nil {
		_ = closed.Close()
	}

	cases := []struct {
		name string
		args []string
		code int
		skip bool
	}{
		{"a file that is not there", []string{"missing.dot"}, 2, false},
		{"a file that may not be opened", []string{"closed.dot"}, 2, err == nil},
		{"an empty name", []string{""}, 2, false},
		{"a flag neither knows", []string{"-Q", "in.dot"}, 1, false},
		{"a format neither knows", []string{"-Tnope", "in.dot"}, 1, false},
		{"a layout neither knows", []string{"-Knope", "in.dot"}, 1, false},
		{"-T without its value", []string{"in.dot", "-T"}, 1, false},
		{"-K without its value", []string{"in.dot", "-K"}, 1, false},
		{"-o without its value", []string{"in.dot", "-o"}, 1, false},
		{"a bad flag before a missing file", []string{"-Tnope", "missing.dot"}, 1, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if c.skip {
				t.Skip("the file opens whatever its mode")
			}

			// A graph waits on standard input: none of these reads it.
			got := dot(t, dir, graph, c.args...)
			if got.code != c.code || got.stdout != "" || !strings.HasPrefix(got.stderr, "dot: ") {
				t.Fatalf(
					"exit %d, stdout %q, stderr %q; want %d, nothing and a message",
					got.code,
					got.stdout,
					got.stderr,
					c.code,
				)
			}

			t.Run("as Graphviz's dot has it", func(t *testing.T) {
				cmd := child(t, nativeDot(t), c.args...)
				cmd.Dir = dir
				cmd.Stdin = strings.NewReader(graph)

				out, err := cmd.Output()

				exit, ok := errors.AsType[*exec.ExitError](err)
				if !ok || exit.ExitCode() != c.code || len(out) != 0 {
					t.Errorf(
						"the native dot wrote %d bytes and returned %v, want nothing and exit %d",
						len(out),
						err,
						c.code,
					)
				}
			})
		})
	}
}

// versionLine is what -V prints: Graphviz's dot's line, with this command's
// version where that one has its build date.
var versionLine = regexp.MustCompile(`^dot - graphviz version (\d+\.\d+\.\d+) \(forkcloser/dot \S+\)\n$`)

// -V prints one line on standard error and exits 0, as Graphviz's dot does,
// and the line has that one's shape, so what reads the Graphviz release out
// of the one reads it out of the other. The release is the one the engine
// embeds.
func TestVersionIsGraphvizsLine(t *testing.T) {
	t.Parallel()

	got := dot(t, t.TempDir(), "", "-V")

	match := versionLine.FindStringSubmatch(got.stderr)
	if got.code != 0 || got.stdout != "" || match == nil {
		t.Fatalf(
			"exit %d, stdout %q, stderr %q; want 0, nothing and the version line",
			got.code,
			got.stdout,
			got.stderr,
		)
	}

	t.Run("the release is the engine's", func(t *testing.T) {
		t.Parallel()

		if pin := enginePin(t); match[1] != pin {
			t.Errorf("-V names Graphviz %s, the engine is built from %s", match[1], pin)
		}
	})

	t.Run("as Graphviz's dot has it", func(t *testing.T) {
		t.Parallel()

		cmd := child(t, nativeDot(t), "-V")

		var stdout, stderr bytes.Buffer

		cmd.Stdout, cmd.Stderr = &stdout, &stderr

		if err := cmd.Run(); err != nil || stdout.Len() != 0 {
			t.Fatalf("the native dot: %v, stdout %q", err, stdout.String())
		}

		// The same words up to the release, one line, a parenthesis after.
		native := regexp.MustCompile(`^dot - graphviz version (\d+\.\d+\.\d+) \(.+\)\n$`).
			FindStringSubmatch(stderr.String())
		if native == nil {
			t.Fatalf("the native dot's line is %q, not the shape -V follows", stderr.String())
		}

		if majorMinor(native[1]) != majorMinor(match[1]) {
			t.Errorf("the native dot is %s, -V names %s", native[1], match[1])
		}
	})
}

// The first of -?, -h and -V on the command line is the whole command, as
// with Graphviz's dot: a bad flag after it is not read, one before it is the
// failure. Each status is held to the native dot when there is one (-h
// apart, which it does not have).
func TestTheFirstOfHelpAndVersionIsTheWholeCommand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args []string
		code int
	}{
		{[]string{"-?"}, 0},
		{[]string{"-?", "-Tnope"}, 0},
		{[]string{"-?", "-V"}, 0},
		{[]string{"-V", "-Tnope"}, 0},
		{[]string{"-V", "-?"}, 0},
		{[]string{"-Tnope", "-?"}, 1},
		{[]string{"-Tnope", "-V"}, 1},
	}

	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			t.Parallel()

			if got := dot(t, t.TempDir(), "", c.args...); got.code != c.code {
				t.Fatalf("exit %d, want %d\nstdout %q\nstderr %q", got.code, c.code, got.stdout, got.stderr)
			}

			t.Run("as Graphviz's dot has it", func(t *testing.T) {
				code := 0

				err := child(t, nativeDot(t), c.args...).Run()
				if exit, ok := errors.AsType[*exec.ExitError](err); ok {
					code = exit.ExitCode()
				} else if err != nil {
					t.Fatal(err)
				}

				if code != c.code {
					t.Errorf("the native dot exits %d, want %d", code, c.code)
				}
			})
		})
	}
}

// drawn is how many graphs a rendering in the dot format holds.
func drawn(out string) int {
	count := 0

	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "digraph ") || strings.HasPrefix(line, "graph ") {
			count++
		}
	}

	return count
}

// outcome is what a dot made of an input: the graphs it drew and its status.
type outcome struct {
	graphs int
	code   int
}

// exitCode is the status behind the error of a command that ran.
func exitCode(err error) int {
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode()
	}

	return 0
}

// An input may hold several graphs, and each is drawn, one after the other.
// A part that does not parse ends the drawing with status 1 and the graphs
// before it written; a graph Graphviz reports an error about is written and
// the ones after it too, with status 1.
//
// Where each graph starts on a line of its own, the count and the status
// are those of Graphviz's dot. Two things it does are not followed: it
// loses a graph that starts on the line another ends on; and once a graph
// names a layout it does not have, it skips each graph after it, good as it
// is, until one names a layout of its own, and draws from that one on.
// Each case says what Graphviz's dot 16.1.1 does with it, and the native
// dot is held to that, so that a release that does otherwise is noticed.
func TestEveryGraphOfTheInputIsDrawn(t *testing.T) {
	t.Parallel()

	const badLabel = `a [shape=record, label="{a|"]`

	for name, tc := range map[string]struct {
		text   string
		graphs int
		code   int
		stderr string // a part of it, when the status is 1
		native outcome
	}{
		"a graph a line":               {"digraph { a }\ndigraph { b }\ngraph { c }\n", 3, 0, "", outcome{3, 0}},
		"a semicolon after each":       {"digraph { a };\ndigraph { b };\n", 2, 0, "", outcome{2, 0}},
		"a semicolon between two":      {"digraph { a }; digraph { b }\n", 2, 0, "", outcome{1, 0}},
		"two on one line":              {"digraph { a } digraph { b }\n", 2, 0, "", outcome{1, 0}},
		"brace against brace":          {"digraph{a}digraph{b}graph{c}", 3, 0, "", outcome{1, 0}},
		"the second does not parse":    {"digraph { a }\ndigraph { b -> }\ndigraph { c }\n", 1, 1, "syntax error in line 2", outcome{1, 1}},
		"a word after the graph":       {"digraph { a }\nnonsense\n", 1, 1, "syntax error in line 2", outcome{1, 1}},
		"the first does not parse":     {"digraph { a -> }\ndigraph { b }\n", 0, 1, "syntax error in line 1", outcome{0, 1}},
		"the first is reported about":  {"digraph { " + badLabel + " }\ndigraph { b }\n", 2, 1, "bad label format", outcome{2, 1}},
		"the second is reported about": {"digraph { b }\ndigraph { " + badLabel + " }\n", 2, 1, "bad label format", outcome{2, 1}},
		"the only one names no layout": {"digraph { layout=nope; a }\n", 0, 1, "nope", outcome{0, 1}},
		"the first names no layout":    {"digraph { layout=nope; a }\ndigraph { b }\n", 1, 1, "nope", outcome{0, 1}},
		"a layout nobody has, then three": {
			"digraph { layout=nope; a }\ndigraph { b }\ndigraph { layout=dot; c }\ndigraph { d }\n", 3, 1, "nope", outcome{2, 1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			got := dot(t, dir, tc.text, "-Tdot")
			if got.code != tc.code || drawn(got.stdout) != tc.graphs {
				t.Fatalf("exit %d and %d graphs, want %d and %d\nstdout %q\nstderr %q",
					got.code, drawn(got.stdout), tc.code, tc.graphs, got.stdout, got.stderr)
			}

			if (tc.code == 0) != (got.stderr == "") || !strings.Contains(got.stderr, tc.stderr) {
				t.Fatalf("stderr %q, want %q in it", got.stderr, tc.stderr)
			}

			// With -o the file gets what standard output got: every
			// rendering, or it is not made when there is none.
			out := filepath.Join(dir, "out.gv")

			toFile := dot(t, dir, tc.text, "-Tdot", "-o", out)
			if toFile.code != tc.code || toFile.stdout != "" {
				t.Fatalf("-o: exit %d, stdout %q; want %d and nothing", toFile.code, toFile.stdout, tc.code)
			}

			written, err := os.ReadFile(out)

			switch {
			case tc.graphs == 0 && !errors.Is(err, os.ErrNotExist):
				t.Fatalf("-o made a file for no rendering: %q, %v", written, err)
			case tc.graphs != 0 && (err != nil || drawn(string(written)) != tc.graphs):
				t.Fatalf("-o: the file holds %d graphs (%v), want %d", drawn(string(written)), err, tc.graphs)
			}

			t.Run("what Graphviz's dot does", func(t *testing.T) {
				out, err := native(t, nativeDot(t), tc.text, "-Tdot")

				if got := (outcome{drawn(string(out)), exitCode(err)}); got != tc.native {
					t.Errorf("the native dot draws %d graphs and exits %d, want %+v", got.graphs, got.code, tc.native)
				}
			})
		})
	}
}

// -K is the layout of every graph of the input, not of the first alone: two
// graphs are drawn as each is drawn alone.
func TestEveryGraphIsLaidOutAsAsked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for _, layout := range []string{"dot", "neato", "circo"} {
		alone := dot(t, dir, graph, "-K"+layout, "-Tdot")
		twice := dot(t, dir, graph+graph, "-K"+layout, "-Tdot")

		if alone.code != 0 || twice.code != 0 || twice.stdout != alone.stdout+alone.stdout {
			t.Errorf(
				"-K%s: two graphs exit %d with %q, want twice what one gives: %q (exit %d)",
				layout, twice.code, twice.stdout, alone.stdout, alone.code,
			)
		}
	}

	if neato, plain := dot(
		t,
		dir,
		graph,
		"-Kneato",
		"-Tdot",
	), dot(
		t,
		dir,
		graph,
		"-Tdot",
	); neato.stdout == plain.stdout {
		t.Error("-Kneato draws the graph as -Kdot does: the test tells nothing apart")
	}
}

// With -o and several graphs, the file holds every rendering: what standard
// output would have held. Graphviz's dot 16.1.1 writes the first graph into
// the file and the others to standard output, which is not followed; the
// native dot is held to it, so that a release that does otherwise is
// noticed.
func TestGraphvizsDotSplitsSeveralGraphsUnderO(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "out.gv")

	out, err := native(t, nativeDot(t), "digraph { a }\ndigraph { b }\ngraph { c }\n", "-Tdot", "-o", file)

	written, readErr := os.ReadFile(file)
	if readErr != nil {
		t.Fatal(readErr)
	}

	if exitCode(err) != 0 || drawn(string(written)) != 1 || drawn(string(out)) != 2 {
		t.Errorf(
			"the native dot exits %d with %d graphs in the file and %d on standard output, want 0, 1 and 2",
			exitCode(err),
			drawn(string(written)),
			drawn(string(out)),
		)
	}
}

// A text with no graph in it, empty, blank or comments alone, is nothing to
// draw and no failure, as with Graphviz's dot: exit 0, nothing on either
// stream, and with -o no file made and the one at the path left as it was.
// A text that cannot be read stays a failure.
func TestATextWithNoGraphIsNothingToDraw(t *testing.T) {
	t.Parallel()

	for name, text := range map[string]string{
		"empty":               "",
		"blank":               " \t\n\n",
		"a line comment":      "// nothing\n",
		"a block comment":     "/* nothing */",
		"a preprocessor line": "# 1 \"nothing\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			for _, format := range []string{"dot", "svg", "png"} {
				got := dot(t, dir, text, "-T"+format)
				if got.code != 0 || got.stdout != "" || got.stderr != "" {
					t.Errorf(
						"-T%s: exit %d, stdout %q, stderr %q; want 0 and nothing",
						format,
						got.code,
						got.stdout,
						got.stderr,
					)
				}
			}

			if got := dot(
				t,
				dir,
				text,
				"-Tsvg",
				"-o",
				"new.svg",
			); got.code != 0 || got.stdout != "" ||
				got.stderr != "" {
				t.Errorf(
					"-o to a new path: exit %d, stdout %q, stderr %q; want 0 and nothing",
					got.code,
					got.stdout,
					got.stderr,
				)
			}

			if entries := names(t, dir); len(entries) != 0 {
				t.Errorf("-o to a new path left %v in the directory, want nothing", entries)
			}

			kept := filepath.Join(dir, "kept.svg")
			if err := os.WriteFile(kept, []byte("previous"), 0o600); err != nil {
				t.Fatal(err)
			}

			if got := dot(
				t,
				dir,
				text,
				"-Tsvg",
				"-o",
				"kept.svg",
			); got.code != 0 || got.stdout != "" ||
				got.stderr != "" {
				t.Errorf(
					"-o over a file: exit %d, stdout %q, stderr %q; want 0 and nothing",
					got.code,
					got.stdout,
					got.stderr,
				)
			}

			if data, err := os.ReadFile(kept); err != nil || string(data) != "previous" {
				t.Errorf("-o over a file left %q (%v), want it as it was", data, err)
			}

			if entries := names(t, dir); len(entries) != 1 {
				t.Errorf("-o over a file left %v in the directory, want the one file", entries)
			}

			t.Run("as Graphviz's dot has it", func(t *testing.T) {
				out, err := native(t, nativeDot(t), text, "-Tsvg")
				if err != nil || len(out) != 0 {
					t.Errorf("the native dot wrote %d bytes and returned %v, want nothing and success", len(out), err)
				}
			})
		})
	}

	for name, text := range map[string]string{"a graph cut short": "digraph { a -> ", "a word": "nothing"} {
		if got := dot(t, t.TempDir(), text, "-Tsvg"); got.code != 1 || got.stdout != "" ||
			!strings.HasPrefix(got.stderr, "dot: parse input:") {
			t.Errorf(
				"%s: exit %d, stdout %q, stderr %q; want 1 and a parse error",
				name,
				got.code,
				got.stdout,
				got.stderr,
			)
		}
	}
}

// Every -o shape: glued and spaced, a bare name, a relative path, an
// absolute path, a path in a missing directory, the input's own path, and
// an existing file, each leaving exactly the output behind and nothing
// else beside it.
func TestOutputPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args func(dir string) []string
		out  func(dir string) string
		code int
	}{
		{
			"glued bare name",
			func(string) []string { return []string{"-Tsvg", "-oout.svg"} },
			func(d string) string { return filepath.Join(d, "out.svg") },
			0,
		},
		{
			"spaced bare name",
			func(string) []string { return []string{"-Tsvg", "-o", "out.svg"} },
			func(d string) string { return filepath.Join(d, "out.svg") },
			0,
		},
		{
			"relative in a subdirectory",
			func(string) []string { return []string{"-Tsvg", "-o", filepath.Join("sub", "out.svg")} },
			func(d string) string { return filepath.Join(d, "sub", "out.svg") },
			0,
		},
		{
			"absolute",
			func(d string) []string { return []string{"-Tsvg", "-o", filepath.Join(d, "abs.svg")} },
			func(d string) string { return filepath.Join(d, "abs.svg") },
			0,
		},
		{
			"a missing directory",
			func(d string) []string { return []string{"-Tsvg", "-o", filepath.Join(d, "nowhere", "out.svg")} },
			func(d string) string { return filepath.Join(d, "nowhere", "out.svg") },
			1,
		},
		{
			"a directory as the output",
			func(d string) []string { return []string{"-Tsvg", "-o", d} },
			func(string) string { return "" },
			1,
		},
		{
			"a name with spaces",
			func(string) []string { return []string{"-Tsvg", "-o", "out put.svg"} },
			func(d string) string { return filepath.Join(d, "out put.svg") },
			0,
		},
		{
			"a name starting with a dash",
			func(string) []string { return []string{"-Tsvg", "-o", "-dash.svg"} },
			func(d string) string { return filepath.Join(d, "-dash.svg") },
			0,
		},
		{
			"an existing output",
			func(string) []string { return []string{"-Tsvg", "-o", "existing.svg"} },
			func(d string) string { return filepath.Join(d, "existing.svg") },
			0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "sub"), 0o750); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(filepath.Join(dir, "existing.svg"), []byte("previous"), 0o600); err != nil {
				t.Fatal(err)
			}

			before := names(t, dir)
			got := dot(t, dir, graph, c.args(dir)...)

			if got.code != c.code {
				t.Fatalf("exit %d, want %d: %s", got.code, c.code, got.stderr)
			}

			if got.stdout != "" {
				t.Fatalf("stdout with -o: %q", got.stdout)
			}

			if c.code == 0 {
				data, err := os.ReadFile(c.out(dir))
				if err != nil || !bytes.HasPrefix(data, []byte("<?xml")) {
					t.Fatalf("the output at %s: %v %q", c.out(dir), err, firstBytes(data))
				}
			}

			// Nothing but the output appears: no temporary file beside it.
			after := names(t, dir)
			for name := range after {
				if before[name] || (c.code == 0 && filepath.Join(dir, name) == c.out(dir)) || name == "sub" {
					continue
				}

				t.Fatalf("left behind: %s", name)
			}

			if sub := names(
				t,
				filepath.Join(dir, "sub"),
			); len(sub) > 1 ||
				(len(sub) == 1 && c.name != "relative in a subdirectory") {
				t.Fatalf("left behind in sub: %v", sub)
			}

			if c.code != 0 && c.name == "a directory as the output" {
				if prev, _ := os.ReadFile(filepath.Join(dir, "existing.svg")); string(prev) != "previous" {
					t.Fatal("a failed run touched another file")
				}
			}
		})
	}
}

// The input's own path as the output: the input is read whole before the
// output is written, so the graph survives into its rendering.
func TestOutputOverTheInput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "graph.dot")

	if err := os.WriteFile(path, []byte(graph), 0o600); err != nil {
		t.Fatal(err)
	}

	got := dot(t, dir, "", "-Tdot", "-o", path, path)
	if got.code != 0 {
		t.Fatalf("exit %d: %s", got.code, got.stderr)
	}

	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("pos=")) {
		t.Fatalf("the rendering over the input: %v %q", err, firstBytes(data))
	}

	if n := len(names(t, dir)); n != 1 {
		t.Fatalf("%d entries beside the output, want 1", n)
	}
}

func names(t *testing.T, dir string) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	out := map[string]bool{}
	for _, entry := range entries {
		out[entry.Name()] = true
	}

	return out
}

func firstBytes(data []byte) string {
	if len(data) > 32 {
		data = data[:32]
	}

	return string(data)
}

// A label Graphviz cannot read is reported while the graph is drawn all the
// same, with the label as text: the rendering is whole, on standard output
// and at -o alike, the message is on standard error and the exit status is
// 1, as dot's is.
func TestReportedErrorWritesTheRenderingAndExitsOne(t *testing.T) {
	t.Parallel()

	const reported = `digraph { n [shape=record, label="{a|"] }`

	dir := t.TempDir()

	onStdout := dot(t, dir, reported, "-Tsvg")
	if onStdout.code != 1 {
		t.Fatalf("exit %d, want 1\nstdout %q\nstderr %q", onStdout.code, onStdout.stdout, onStdout.stderr)
	}

	if !strings.HasPrefix(onStdout.stdout, "<?xml") || !strings.HasSuffix(onStdout.stdout, "</svg>\n") {
		t.Fatalf("standard output is not a whole SVG: %q", onStdout.stdout)
	}

	if !strings.HasPrefix(onStdout.stderr, "dot: ") || !strings.Contains(onStdout.stderr, "bad label format") {
		t.Fatalf("stderr does not carry Graphviz's message: %q", onStdout.stderr)
	}

	out := filepath.Join(dir, "out.svg")
	if err := os.WriteFile(out, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}

	toFile := dot(t, dir, reported, "-Tsvg", "-o", out)
	if toFile.code != 1 {
		t.Fatalf("-o: exit %d, want 1\nstdout %q\nstderr %q", toFile.code, toFile.stdout, toFile.stderr)
	}

	if toFile.stdout != "" || toFile.stderr != onStdout.stderr {
		t.Fatalf("-o: stdout %q, stderr %q, want none and %q", toFile.stdout, toFile.stderr, onStdout.stderr)
	}

	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	if string(written) != onStdout.stdout {
		t.Fatalf("-o holds %q, want the rendering standard output got", firstBytes(written))
	}

	if n := len(names(t, dir)); n != 1 {
		t.Fatalf("%d entries beside the output, want 1", n)
	}
}
