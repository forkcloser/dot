//go:build unix

package main

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// unixOutputCases are the cases of where -o writes that need a device, a
// pipe or a file mode.
func unixOutputCases() []outputCase {
	return []outputCase{
		{"the null device", 0, func(t *testing.T, _ string) (string, func(*testing.T)) {
			t.Helper()

			return os.DevNull, func(t *testing.T) {
				t.Helper()

				if info, err := os.Lstat(os.DevNull); err != nil || info.Mode()&fs.ModeCharDevice == 0 {
					t.Errorf("the null device is no longer one: %v, %v", info, err)
				}
			}
		}},
		{"a pipe with a reader", 0, func(t *testing.T, dir string) (string, func(*testing.T)) {
			t.Helper()

			pipe := filepath.Join(dir, "pipe")
			if err := syscall.Mkfifo(pipe, 0o600); err != nil {
				t.Skipf("no named pipe here: %v", err)
			}

			type read struct {
				data []byte
				err  error
			}

			done := make(chan read, 1)

			go func() {
				file, err := os.Open(pipe)
				if err != nil {
					done <- read{nil, err}

					return
				}

				data, err := io.ReadAll(file)
				_ = file.Close()

				done <- read{data, err}
			}()

			return pipe, func(t *testing.T) {
				t.Helper()

				got := <-done
				rendered(t, "the reader of the pipe", got.data, got.err)

				if info, err := os.Lstat(pipe); err != nil || info.Mode()&fs.ModeNamedPipe == 0 {
					t.Errorf("the pipe is no longer one: %v, %v", info, err)
				}
			}
		}},
		{"an existing file keeps its mode", 0, func(t *testing.T, dir string) (string, func(*testing.T)) {
			t.Helper()

			out := filepath.Join(dir, "out.svg")
			if err := os.WriteFile(out, []byte("previous"), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := os.Chmod(out, 0o660); err != nil {
				t.Fatal(err)
			}

			return out, func(t *testing.T) {
				t.Helper()

				data, err := os.ReadFile(out)
				rendered(t, "the file", data, err)

				if info, err := os.Stat(out); err != nil || info.Mode().Perm() != 0o660 {
					t.Errorf("the file's mode is %v (%v), want 0660 as it was", info.Mode().Perm(), err)
				}
			}
		}},
		{"an existing file keeps its sticky bit", 0, func(t *testing.T, dir string) (string, func(*testing.T)) {
			t.Helper()

			const mode = 0o644 | fs.ModeSticky

			out := filepath.Join(dir, "out.svg")
			if err := os.WriteFile(out, []byte("previous"), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := os.Chmod(out, mode); err != nil {
				t.Fatal(err)
			}

			if info, err := os.Stat(out); err != nil || info.Mode()&fs.ModeSticky == 0 {
				t.Skipf("a file takes no sticky bit here: %v, %v", info, err)
			}

			return out, func(t *testing.T) {
				t.Helper()

				data, err := os.ReadFile(out)
				rendered(t, "the file", data, err)

				if info, err := os.Stat(out); err != nil || info.Mode()&(fs.ModePerm|fs.ModeSticky) != mode {
					t.Errorf("the file's mode is %v (%v), want %v as it was", info.Mode(), err, mode)
				}
			}
		}},
		{"an existing file keeps its group", 0, func(t *testing.T, dir string) (string, func(*testing.T)) {
			t.Helper()

			out := filepath.Join(dir, "out.svg")
			if err := os.WriteFile(out, []byte("previous"), 0o600); err != nil {
				t.Fatal(err)
			}

			// A group of the user's that a new file here does not get.
			groups, err := os.Getgroups()
			if err != nil {
				t.Skipf("the user's groups: %v", err)
			}

			group := -1

			for _, candidate := range groups {
				if uint32(candidate) != groupOf(t, out) {
					group = candidate

					break
				}
			}

			if group < 0 || os.Chown(out, -1, group) != nil {
				t.Skip("the user has no second group to give a file")
			}

			return out, func(t *testing.T) {
				t.Helper()

				data, err := os.ReadFile(out)
				rendered(t, "the file", data, err)

				if got := groupOf(t, out); got != uint32(group) {
					t.Errorf("the file's group is %d, want %d as it was", got, group)
				}
			}
		}},
		{"a new file takes the mode the umask leaves", 0, func(t *testing.T, dir string) (string, func(*testing.T)) {
			t.Helper()

			// What this process gets for a new file asked for as 0666.
			probe := filepath.Join(dir, "probe")

			file, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
			if err != nil {
				t.Fatal(err)
			}

			_ = file.Close()

			want, err := os.Stat(probe)
			if err != nil {
				t.Fatal(err)
			}

			out := filepath.Join(dir, "out.svg")

			return out, func(t *testing.T) {
				t.Helper()

				data, err := os.ReadFile(out)
				rendered(t, "the file", data, err)

				if info, err := os.Stat(out); err != nil || info.Mode().Perm() != want.Mode().Perm() {
					t.Errorf("the file's mode is %v (%v), want %v", info.Mode().Perm(), err, want.Mode().Perm())
				}
			}
		}},
		{"a file in a directory nothing can be made in", 0, func(t *testing.T, dir string) (string, func(*testing.T)) {
			t.Helper()

			closed := filepath.Join(dir, "closed")
			if err := os.Mkdir(closed, 0o700); err != nil {
				t.Fatal(err)
			}

			out := filepath.Join(closed, "out.svg")
			if err := os.WriteFile(out, []byte("previous"), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := os.Chmod(closed, 0o500); err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = os.Chmod(closed, 0o700) })

			if probe, err := os.Create(filepath.Join(closed, "probe")); err == nil {
				_ = probe.Close()

				t.Skip("a file can be made in the directory whatever its mode")
			}

			return out, func(t *testing.T) {
				t.Helper()

				data, err := os.ReadFile(out)
				rendered(t, "the file", data, err)
			}
		}},
	}
}

// groupOf is the group of the file at path.
func groupOf(t *testing.T, path string) uint32 {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("no group to read here")
	}

	return stat.Gid
}
