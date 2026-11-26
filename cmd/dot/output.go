package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/forkcloser/go-xos"
)

// output is the file -o names, written as Graphviz's dot writes it wherever
// that can be told from outside: what is at the path keeps its kind, its
// names, its mode, its owner and its group, and a file the user may not
// write is not written.
//
// Nothing is opened before the first rendering is written, so an input
// with nothing to draw, or whose first graph fails, makes no file and
// leaves the one at the path as it was.
//
// Where it loses nothing of the above, the renderings go to a temporary
// file beside the output that is renamed onto it once the drawing is over:
// an interrupt or a full disk then leaves the old file whole, where a file
// written in place is left cut short. That is so for a path with nothing
// at it, and for a regular file with one name, owned by the user, in a
// directory a file can be made in. Anything else is written in place: a
// device or a pipe, which a rename would replace with a regular file; a
// file with another name, which would keep the old content; a file of
// another owner, or of a group the user cannot give a file; a file whose
// mode has a bit beyond its permissions; a file in a directory nothing can
// be made in. What a file replaced by the rename does not keep is its
// extended attributes and its access control list, which are not read.
type output struct {
	path string
	// file is nil until the first write.
	file *os.File
	// target is the path written: path, or what path names as a symbolic
	// link, which stays a link.
	target string
	// temp is the temporary file to rename onto target, or empty when
	// target is written in place.
	temp string
}

// tempAttempts is how many names are tried for the temporary file.
const tempAttempts = 100

// newFilePerm is the mode a new output is asked for, before the umask.
const newFilePerm = 0o666

// beyondPerm are the bits of a mode a temporary file is not given: a file
// that has one is written in place, where the system decides what a write
// leaves of them.
const beyondPerm = fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

func (o *output) Write(p []byte) (int, error) {
	if o.file == nil {
		if err := o.open(); err != nil {
			return 0, err
		}
	}

	//nolint:wrapcheck // os's *PathError already names the operation and the path
	return o.file.Write(p)
}

// open opens the file the renderings go to: the temporary beside the
// target, or the target itself.
func (o *output) open() error {
	o.target = o.path

	// A path that cannot be resolved names nothing yet, or a link whose
	// file is not there: it is opened as it is written, which makes it.
	if resolved, err := filepath.EvalSymlinks(o.path); err == nil {
		o.target = resolved
	}

	info, err := os.Lstat(o.target)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Where no temporary file can be made, opening the path says
		// why in the user's own terms, or makes the file.
		if o.openTemp(nil) != nil {
			return o.openInPlace()
		}

		return nil
	case err != nil:
		//nolint:wrapcheck // os's *PathError already names the operation and the path
		return err
	case !info.Mode().IsRegular():
		return o.openInPlace()
	}

	// The file's own mode decides whether it may be written; a rename
	// would walk past it.
	probe, err := xos.OpenFile(o.target, os.O_WRONLY, 0)
	if err != nil {
		//nolint:wrapcheck // os's *PathError already names the operation and the path
		return err
	}

	_ = probe.Close()

	if info.Mode()&beyondPerm != 0 || keepsItsFile(o.target, info) || o.openTemp(info) != nil {
		return o.openInPlace()
	}

	return nil
}

// openInPlace opens the target itself, emptied.
func (o *output) openInPlace() error {
	file, err := xos.OpenFile(o.target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, newFilePerm)
	if err != nil {
		//nolint:wrapcheck // os's *PathError already names the operation and the path
		return err
	}

	o.file = file

	return nil
}

// openTemp makes the temporary file beside the target: one with the mode
// the umask leaves a new file, or, when the target is there, one with the
// mode and the group of existing.
func (o *output) openTemp(existing os.FileInfo) error {
	dir, base := filepath.Split(o.target)
	if dir == "" {
		// A bare name: beside it is the working directory.
		dir = "."
	}

	perm := fs.FileMode(newFilePerm)
	if existing != nil {
		perm = existing.Mode().Perm()
	}

	for range tempAttempts {
		name := filepath.Join(dir, "."+base+"."+rand.Text())

		file, err := xos.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if errors.Is(err, fs.ErrExist) {
			continue
		}

		if err != nil {
			//nolint:wrapcheck // os's *PathError already names the operation and the path
			return err
		}

		if existing != nil {
			if err := takeAfter(file, existing); err != nil {
				_ = file.Close()
				_ = os.Remove(name)

				return err
			}
		}

		o.file, o.temp = file, name

		return nil
	}

	return fmt.Errorf("no name left for a temporary file beside %s: %w", o.target, fs.ErrExist)
}

// takeAfter gives file the group and the mode of existing, the file it is
// to replace.
func takeAfter(file *os.File, existing os.FileInfo) error {
	// The group first: a change of group may clear bits of the mode.
	if err := takeGroup(file, existing); err != nil {
		return err
	}

	//nolint:wrapcheck // os's *PathError already names the operation and the path
	return file.Chmod(existing.Mode().Perm())
}

// finish closes what was written and, when it went to a temporary file,
// renames it onto the target. Without a write it does nothing.
func (o *output) finish() error {
	if o.file == nil {
		return nil
	}

	file := o.file
	o.file = nil

	if err := file.Close(); err != nil {
		o.discard()

		//nolint:wrapcheck // os's *PathError already names the operation and the path
		return err
	}

	if o.temp == "" {
		return nil
	}

	if err := xos.Rename(o.temp, o.target); err != nil {
		o.discard()

		//nolint:wrapcheck // os's *LinkError already names the operation and both paths
		return err
	}

	o.temp = ""

	return nil
}

// discard closes the output and removes the temporary file, if there is
// one: what was at the target stays.
func (o *output) discard() {
	if o.file != nil {
		_ = o.file.Close()
		o.file = nil
	}

	if o.temp != "" {
		_ = os.Remove(o.temp)
		o.temp = ""
	}
}
