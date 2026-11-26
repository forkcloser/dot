//go:build unix

package main

import (
	"os"
	"syscall"
)

// keepsItsFile reports whether the regular file info describes has to stay
// the file it is: it has another name, or an owner a new file would not
// have.
func keepsItsFile(_ string, info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}

	return stat.Nlink > 1 || int(stat.Uid) != os.Geteuid()
}

// takeGroup gives file the group of existing when it has another: a new
// file takes its directory's group on some systems and the process's on
// others. It fails for a group the user cannot give a file.
func takeGroup(file *os.File, existing os.FileInfo) error {
	want, ok := existing.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}

	info, err := file.Stat()
	if err != nil {
		//nolint:wrapcheck // os's *PathError already names the operation and the path
		return err
	}

	if got, ok := info.Sys().(*syscall.Stat_t); ok && got.Gid == want.Gid {
		return nil
	}

	//nolint:wrapcheck // os's *PathError already names the operation and the path
	return file.Chown(-1, int(want.Gid))
}
