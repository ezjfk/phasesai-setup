package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var harnessNames = map[string]string{"claude": "Claude Code", "codex": "OpenAI Codex", "opencode": "OpenCode"}
var stableVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func versionOutput(harness, output string) (string, error) {
	value := strings.TrimSpace(output)
	switch harness {
	case "claude":
		value = strings.TrimSuffix(value, " (Claude Code)")
	case "codex":
		value = strings.TrimPrefix(value, "codex-cli ")
	case "opencode":
	default:
		return "", errors.New("unsupported harness")
	}
	if !stableVersion.MatchString(value) {
		return "", errors.New("unexpected harness version output")
	}
	return value, nil
}

func harnessCommand(ctx context.Context, exe string, args ...string) (*exec.Cmd, error) {
	if !strings.EqualFold(filepath.Ext(exe), ".exe") {
		return nil, errors.New("a native Windows executable is required")
	}
	return exec.CommandContext(ctx, exe, args...), nil
}

func checkHarness(exe, harness string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd, err := harnessCommand(ctx, exe, "--version")
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	cmd.Stdout = &limitedWriter{Writer: &out, Remaining: 4096}
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s could not start; rerun setup to repair its native installation", harnessNames[harness])
	}
	return versionOutput(harness, out.String())
}

type installation struct {
	Version    string `json:"version"`
	Executable string `json:"executable"`
}

func installRoot(home string) (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(home, "AppData", "Local")
	}
	root := filepath.Join(base, "PhasesAI")
	if err := safePath(home, filepath.Join(root, "installations.json")); err != nil {
		return "", err
	}
	return root, nil
}

func managedHarness(home, harness string) (string, error) {
	if _, ok := harnessNames[harness]; !ok {
		return "", errors.New("unsupported harness")
	}
	root, err := installRoot(home)
	if err != nil {
		return "", err
	}
	data, _, err := readPreview(home, filepath.Join(root, harness+"-installed.json"))
	if err != nil {
		return "", err
	}
	var installed installation
	if json.Unmarshal(data, &installed) != nil || !filepath.IsLocal(installed.Executable) {
		return "", errors.New("run setup to install this harness first")
	}
	exe := filepath.Join(root, installed.Executable)
	if err := safePath(home, exe); err != nil {
		return "", err
	}
	return exe, nil
}

func launchHarness(harness string, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	exe, err := managedHarness(home, harness)
	if err != nil {
		return err
	}
	cmd, err := harnessCommand(context.Background(), exe, args...)
	if err != nil {
		return err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.New("harness exited unsuccessfully; inspect its terminal output")
	}
	return nil
}

type limitedWriter struct {
	Writer    interface{ Write([]byte) (int, error) }
	Remaining int
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if len(data) > w.Remaining {
		return 0, errors.New("command output limit exceeded")
	}
	w.Remaining -= len(data)
	return w.Writer.Write(data)
}
