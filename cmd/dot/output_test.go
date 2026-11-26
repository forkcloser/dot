package main

// Where -o writes: what is at the path keeps its kind, its names and its
// mode, and a file the user may not write is not written, as with
// Graphviz's dot. Each case runs the command, then, when there is a native
// dot, runs that one through the same set-up and the same checks.

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rendered fails the test unless data is an SVG rendering, whole and with
// nothing after it. Its last line ends as the writer ends lines: Graphviz's
// dot writes a text format through the C library's text mode, which on
// Windows is a carriage return before each line feed.
func rendered(t *testing.T, what string, data []byte, err error) {
	t.Helper()

	whole := bytes.HasSuffix(data, []byte("</svg>\n")) || bytes.HasSuffix(data, []byte("</svg>\r\n"))
	if err != nil || !bytes.HasPrefix(data, []byte("<?xml")) || !whole {
		t.Errorf(
			"%s holds %q ... %q (%v), want the rendering alone",
			what,
			firstBytes(data),
			data[max(0, len(data)-16):],
			err,
		)
	}
}

// outputCase prepares dir and returns the path -o is given and what to
// check once the command has run. It skips where the platform, or the
// user's rights, do not make the case.
type outputCase struct {
	name  string
	code  int
	setUp func(t *testing.T, dir string) (out string, check func(t *testing.T))
}

func outputCases() []outputCase {
	return []outputCase{
		{"a file with a second name", 0, func(t *testing.T, dir string) (string, func(*testing.T)) {
			t.Helper()

			// Longer than the rendering: a file written in place is emptied
			// first.
			first, second := filepath.Join(dir, "first.svg"), filepath.Join(dir, "second.svg")
			if err := os.WriteFile(first, bytes.Repeat([]byte("previous"), 1<<14), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := os.Link(first, second); err != nil {
				t.Skipf("no hard link here: %v", err)
			}

			return first, func(t *testing.T) {
				t.Helper()

				for _, name := range []string{first, second} {
					data, err := os.ReadFile(name)
					rendered(t, filepath.Base(name), data, err)
				}
			}
		}},
		{"a symbolic link", 0, func(t *testing.T, dir string) (string, func(*testing.T)) {
			t.Helper()

			target, link := filepath.Join(dir, "target.svg"), filepath.Join(dir, "link.svg")
			if err := os.WriteFile(target, []byte("previous"), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := os.Symlink("target.svg", link); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}

			return link, func(t *testing.T) {
				t.Helper()

				if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
					t.Errorf("the link is no longer one: %v, %v", info, err)
				}

				data, err := os.ReadFile(target)
				rendered(t, "the link's target", data, err)
			}
		}},
		{"a symbolic link to a file that is not there", 0, func(t *testing.T, dir string) (string, func(*testing.T)) {
			t.Helper()

			target, link := filepath.Join(dir, "target.svg"), filepath.Join(dir, "link.svg")
			if err := os.Symlink("target.svg", link); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}

			return link, func(t *testing.T) {
				t.Helper()

				if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
					t.Errorf("the link is no longer one: %v, %v", info, err)
				}

				data, err := os.ReadFile(target)
				rendered(t, "the file the link names", data, err)
			}
		}},
		{"a file that may not be written", 1, func(t *testing.T, dir string) (string, func(*testing.T)) {
			t.Helper()

			out := filepath.Join(dir, "closed.svg")
			if err := os.WriteFile(out, []byte("previous"), 0o400); err != nil {
				t.Fatal(err)
			}

			// So that the directory can be removed where a read-only file
			// stops that.
			t.Cleanup(func() { _ = os.Chmod(out, 0o600) })

			if file, err := os.OpenFile(out, os.O_WRONLY, 0); err == nil {
				_ = file.Close()

				t.Skip("the file opens for writing whatever its mode")
			}

			return out, func(t *testing.T) {
				t.Helper()

				if data, err := os.ReadFile(out); err != nil || string(data) != "previous" {
					t.Errorf("the file holds %q (%v), want it as it was", firstBytes(data), err)
				}

				if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
					t.Errorf("%d entries in the directory (%v), want the file alone", len(entries), err)
				}
			}
		}},
	}
}

func TestOutputIsWrittenAsGraphvizsDotWritesIt(t *testing.T) {
	t.Parallel()

	for _, c := range append(outputCases(), unixOutputCases()...) {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			out, check := c.setUp(t, dir)

			got := dot(t, dir, graph, "-Tsvg", "-o", out)
			if got.code != c.code || got.stdout != "" || (c.code == 0) != (got.stderr == "") {
				t.Fatalf("exit %d, stdout %q, stderr %q; want %d", got.code, got.stdout, got.stderr, c.code)
			}

			check(t)

			t.Run("as Graphviz's dot has it", func(t *testing.T) {
				nativePath := nativeDot(t)

				dir := t.TempDir()
				out, check := c.setUp(t, dir)

				cmd := child(t, nativePath, "-Tsvg", "-o", out)
				cmd.Dir = dir
				cmd.Stdin = bytes.NewReader([]byte(graph))

				code := 0

				err := cmd.Run()
				if exit, ok := errors.AsType[*exec.ExitError](err); ok {
					code = exit.ExitCode()
				} else if err != nil {
					t.Fatal(err)
				}

				if code != c.code {
					t.Errorf("the native dot exits %d, want %d", code, c.code)
				}

				check(t)
			})
		})
	}
}

// Where the renderings go to a temporary file, what is at the path is as
// it was until the drawing is over: a command killed while it draws leaves
// the file there whole, or no file where there was none, where one that
// wrote in place would leave a file cut short.
func TestAKilledDrawingLeavesThePathAsItWas(t *testing.T) {
	t.Parallel()

	const (
		previous = "previous"
		// Enough graphs that the drawing outlasts the test's look at it.
		many = 20000
	)

	// holds says what is wrong with the file at path, which is to hold
	// previous; nothing when it does.
	holds := func(path string) string {
		if data, err := os.ReadFile(path); err != nil || string(data) != previous {
			return fmt.Sprintf("%s holds %q (%v), want it as it was", filepath.Base(path), firstBytes(data), err)
		}

		return ""
	}

	for name, setUp := range map[string]func(t *testing.T, dir string) (out string, wrong func() string){
		"over a file": func(t *testing.T, dir string) (string, func() string) {
			t.Helper()

			out := filepath.Join(dir, "out.svg")
			if err := os.WriteFile(out, []byte(previous), 0o600); err != nil {
				t.Fatal(err)
			}

			return out, func() string { return holds(out) }
		},
		"through a symbolic link": func(t *testing.T, dir string) (string, func() string) {
			t.Helper()

			target, link := filepath.Join(dir, "target.svg"), filepath.Join(dir, "link.svg")
			if err := os.WriteFile(target, []byte(previous), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := os.Symlink("target.svg", link); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}

			return link, func() string {
				if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
					return fmt.Sprintf("the link is no longer one: %v, %v", info, err)
				}

				return holds(target)
			}
		},
		"where nothing is": func(_ *testing.T, dir string) (string, func() string) {
			out := filepath.Join(dir, "out.svg")

			return out, func() string {
				if _, err := os.Lstat(out); !errors.Is(err, fs.ErrNotExist) {
					return fmt.Sprintf("something is at the path (%v), want nothing", err)
				}

				return ""
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			out, wrong := setUp(t, dir)

			before, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}

			cmd := child(t, binary, "-Tsvg", "-o", out)
			cmd.Dir = dir
			cmd.Stdin = strings.NewReader(strings.Repeat(graph, many))

			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}

			ended := make(chan error, 1)

			go func() { ended <- cmd.Wait() }()

			asItWas := func(when string) {
				t.Helper()

				if what := wrong(); what != "" {
					_ = cmd.Process.Kill()

					t.Fatalf("%s: %s", when, what)
				}
			}

			// The command is drawing once something new is in the
			// directory: its temporary file.
			for drawing := false; !drawing; time.Sleep(time.Millisecond) {
				select {
				case err := <-ended:
					t.Fatalf("the command ended before it was seen drawing: %v", err)
				default:
				}

				asItWas("before the first rendering")

				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}

				drawing = len(entries) > len(before)
			}

			asItWas("while the command draws")

			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}

			if err := <-ended; err == nil {
				t.Error("the command ended with status 0, killed as it was")
			}

			asItWas("after the command was killed")
		})
	}
}

// An output that cannot be made is an error in the user's own terms: the
// path given, not a temporary file's beside it.
func TestAnOutputThatCannotBeMadeIsNamedAsGiven(t *testing.T) {
	t.Parallel()

	out := filepath.Join("nowhere", "out.svg")

	got := dot(t, t.TempDir(), graph, "-Tsvg", "-o", out)
	if got.code != statusFailed || !strings.Contains(got.stderr, out) || strings.Contains(got.stderr, ".out.svg.") {
		t.Errorf("exit %d, stderr %q; want 1 and an error that names %s", got.code, got.stderr, out)
	}
}
