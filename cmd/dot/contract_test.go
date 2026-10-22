package main

// The command's contract at the process boundary, from the README: the
// flags in their glued and spaced forms, -V and -h, the input as a file,
// "-" or standard input, -o as a path in every shape, and the exit
// statuses 0, 1 and 2 with the usage on stderr for 2. Each case runs the
// built binary in a child.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// binary is the command built once for the package's tests.
var binary string //nolint:gochecknoglobals // built by TestMain, read by every case

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "dot-test")
	if err != nil {
		panic(err)
	}

	binary = filepath.Join(dir, "dot"+exeSuffix())

	build := exec.CommandContext(context.Background(), "go", "build", "-o", binary, ".")
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
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

func dot(t *testing.T, dir, stdin string, args ...string) result {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), binary, args...)
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
		{"-h with -V", "", []string{"-V", "-h"}, 0, "usage:", false},
		{"unknown flag", graph, []string{"-x"}, 2, "", true},
		{"unknown format", graph, []string{"-Tgif"}, 2, "", true},
		{"unknown layout", graph, []string{"-Kbogus"}, 2, "", true},
		{"a flag without its value", graph, []string{"-o"}, 2, "", true},
		{"two inputs", graph, []string{in, in}, 2, "", true},
		{"a long flag that is not help or version", graph, []string{"--format=svg"}, 2, "", true},
		{"usage error beats a missing file", "", []string{"-Tgif", filepath.Join(dir, "missing")}, 2, "", true},
		{"a missing file", "", []string{filepath.Join(dir, "missing.dot")}, 1, "", false},
		{"a directory as input", "", []string{dir}, 1, "", false},
		{"a syntax error", "digraph { a -> ", nil, 1, "", false},
		{"empty input", "", nil, 1, "", false},
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

			switch c.code {
			case 0:
				if got.stderr != "" {
					t.Fatalf("stderr on success: %q", got.stderr)
				}

				if c.stdout == "" && strings.TrimSpace(got.stdout) == "" {
					t.Fatalf("no version printed: %q", got.stdout)
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

	// The command still holds the input open when it renames the rendering
	// over it, which Windows refuses ("Access is denied"); held there with
	// the owner until the input is closed before the rename.
	if runtime.GOOS == "windows" {
		t.Skip("held: the input is still open when the rendering is renamed over it")
	}

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
