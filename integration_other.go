//go:build !windows

package main

import "errors"

func enableConsoleColor() bool                         { return false }
func windowsArchitecture() string                      { return "amd64" }
func ownConsole() bool                                 { return false }
func integrateWindows(home, harness, exe string) error { return errors.New("Windows is required") }
