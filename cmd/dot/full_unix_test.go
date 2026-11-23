//go:build unix

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A write of a rendering that fails ends the drawing, with one error and
// status 1, and with -o nothing goes to the path: no file is made, and the
// one there stays as it was. Going on would write the graphs after it to an
// output that has a rendering cut short in it.
//
// The write is made to fail as on a full disk: the command runs under a
// shell that holds the files it writes to a few kilobytes, which the first
// rendering fits in and the second does not.
func TestAFailedWriteEndsTheDrawing(t *testing.T) {
	t.Parallel()

	const (
		previous = "previous"
		limited  = `ulimit -f 8; exec "$0" "$@"`
	)

	var text strings.Builder

	text.WriteString("digraph { a }\ndigraph {")

	for range 200 {
		text.WriteString(" a -> b;")
	}

	text.WriteString(" }\ndigraph { c }\n")

	run := func(t *testing.T, dir, script string, args ...string) {
		t.Helper()

		cmd := child(t, "sh", append([]string{"-c", script, binary}, args...)...)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(text.String())

		var stdout, stderr bytes.Buffer

		cmd.Stdout, cmd.Stderr = &stdout, &stderr

		err := cmd.Run()
		said := stderr.String()

		if exitCode(err) != statusFailed || stdout.Len() != 0 ||
			!strings.HasPrefix(said, "dot: ") || strings.Count(said, "\n") != 1 {
			t.Fatalf(
				"exit %d (%v), stdout %q, stderr %q; want 1 and one error",
				exitCode(err),
				err,
				stdout.String(),
				said,
			)
		}
	}

	holds := func(t *testing.T, dir string, names ...string) {
		t.Helper()

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}

		got := make([]string, 0, len(entries))
		for _, entry := range entries {
			got = append(got, entry.Name())
		}

		if strings.Join(got, " ") != strings.Join(names, " ") {
			t.Errorf("the directory holds %q, want %q", got, names)
		}
	}

	t.Run("-o over a file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		out := filepath.Join(dir, "out.svg")
		if err := os.WriteFile(out, []byte(previous), 0o600); err != nil {
			t.Fatal(err)
		}

		run(t, dir, limited, "-Tsvg", "-o", "out.svg")
		holds(t, dir, "out.svg")

		if data, err := os.ReadFile(out); err != nil || string(data) != previous {
			t.Errorf("the file holds %q (%v), want it as it was", firstBytes(data), err)
		}
	})

	t.Run("-o where nothing is", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		run(t, dir, limited, "-Tsvg", "-o", "out.svg")
		holds(t, dir)
	})

	t.Run("standard output", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		run(t, dir, limited+" > out.svg", "-Tsvg")
		holds(t, dir, "out.svg")
	})
}
