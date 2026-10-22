//go:build windows

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func enableConsoleColor() bool {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	return windows.GetConsoleMode(h, &mode) == nil && windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}

func windowsArchitecture() string {
	if runtime.GOARCH == "arm64" || strings.EqualFold(os.Getenv("PROCESSOR_ARCHITEW6432"), "ARM64") || strings.EqualFold(os.Getenv("PROCESSOR_ARCHITECTURE"), "ARM64") {
		return "arm64"
	}
	return "amd64"
}

func ownConsole() bool {
	var ids [2]uint32
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&ids[0])), 2)
	return n == 1
}

func updateUserPath(current, directory, harnessRoot string) string {
	parts := []string{directory}
	for _, p := range strings.Split(current, ";") {
		p = strings.TrimSpace(p)
		if p == "" || strings.EqualFold(filepath.Clean(p), filepath.Clean(directory)) {
			continue
		}
		rel, err := filepath.Rel(harnessRoot, p)
		if err == nil && filepath.IsLocal(rel) {
			continue
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ";")
}

func powershellLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
func encodedPowerShell(script string) string {
	words := utf16.Encode([]rune(script))
	b := make([]byte, len(words)*2)
	for i, word := range words {
		b[2*i], b[2*i+1] = byte(word), byte(word>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func integrateWindows(home, harness, exe string) error {
	root, err := installRoot(home)
	if err != nil {
		return err
	}
	envKey, _, err := registry.CreateKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer envKey.Close()
	current, typ, err := envKey.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	updated := updateUserPath(current, filepath.Dir(exe), filepath.Join(root, "harnesses", harness))
	if typ == registry.SZ {
		err = envKey.SetStringValue("Path", updated)
	} else {
		err = envKey.SetExpandStringValue("Path", updated)
	}
	if err != nil {
		return err
	}
	message, _ := windows.UTF16PtrFromString("Environment")
	windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(0xffff, 0x1a, 0, uintptr(unsafe.Pointer(message)), 2, 5000, 0)
	_ = os.Setenv("PATH", filepath.Dir(exe)+";"+os.Getenv("PATH"))
	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	shortcut := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "PhasesAI", harnessNames[harness]+".lnk")
	if err := safePath(home, shortcut); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(shortcut), 0700); err != nil {
		return err
	}
	// Built-in Windows PowerShell, literal paths, and no credentials in arguments.
	script := "$ErrorActionPreference='Stop'; $shell=New-Object -ComObject WScript.Shell; $link=$shell.CreateShortcut(" + powershellLiteral(shortcut) + "); $link.TargetPath=" + powershellLiteral(exe) + "; $link.WorkingDirectory=" + powershellLiteral(home) + "; $link.Description='Configured with PhasesAI'; $link.Save()"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ps := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, ps, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodedPowerShell(script))
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		return errors.New("could not create Start menu shortcut")
	}
	return nil
}
