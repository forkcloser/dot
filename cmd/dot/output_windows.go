package main

import (
	"os"
	"syscall"

	"github.com/forkcloser/go-xos"
)

// keepsItsFile reports whether the regular file at path has to stay the
// file it is: it has another name.
func keepsItsFile(path string, _ os.FileInfo) bool {
	file, err := xos.Open(path)
	if err != nil {
		return true
	}

	defer func() { _ = file.Close() }()

	var info syscall.ByHandleFileInformation

	if err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &info); err != nil {
		return true
	}

	return info.NumberOfLinks > 1
}

// takeGroup does nothing: a file has no group to keep here.
func takeGroup(*os.File, os.FileInfo) error { return nil }
