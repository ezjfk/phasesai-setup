//go:build linux || darwin

package main

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func isReparse(info os.FileInfo) bool { return false }
func inspectFile(f *os.File) (fileMetadata, error) {
	info, err := f.Stat()
	if err != nil {
		return fileMetadata{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return fileMetadata{}, errors.New("file ownership, type or hard-link count is unsafe")
	}
	return fileMetadata{Mode: uint32(info.Mode().Perm())}, nil
}
func secureFile(f *os.File) error                      { return f.Chmod(0600) }
func restoreMetadata(f *os.File, m fileMetadata) error { return f.Chmod(os.FileMode(m.Mode & 0777)) }
func lockFile(f *os.File) error                        { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func syncDirectory(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
