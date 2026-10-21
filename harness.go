package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type harnessRelease struct{ Command, Package, Version string }

var harnessReleases = map[string]harnessRelease{
	"claude":   {"claude", "@anthropic-ai/claude-code", "2.1.306"},
	"codex":    {"codex", "@openai/codex", "0.163.0"},
	"opencode": {"opencode", "opencode-ai", "1.19.3"},
}

func reviewedVersionOutput(harness, output string) bool {
	release, ok := harnessReleases[harness]
	if !ok {
		return false
	}
	value := strings.TrimSpace(output)
	switch harness {
	case "claude":
		return value == release.Version+" (Claude Code)"
	case "codex":
		return value == "codex-cli "+release.Version
	case "opencode":
		return value == release.Version
	}
	return false
}

func externalCommand(ctx context.Context, exe string, args ...string) *exec.Cmd {
	// The only batch command used here is the fixed npm package installation.
	// No model IDs, keys, paths or other free-form input enter cmd.exe syntax.
	if runtime.GOOS == "windows" && (strings.HasSuffix(strings.ToLower(exe), ".cmd") || strings.HasSuffix(strings.ToLower(exe), ".bat")) {
		return exec.CommandContext(ctx, "cmd.exe", append([]string{"/d", "/c", exe}, args...)...)
	}
	return exec.CommandContext(ctx, exe, args...)
}

func harnessCommand(ctx context.Context, exe string, args ...string) (*exec.Cmd, error) {
	if runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(exe), ".cmd") {
		// Resolve the known npm shim to its vendor entrypoint. Free-form harness
		// arguments must never enter cmd.exe syntax.
		var entry string
		for _, release := range harnessReleases {
			if strings.EqualFold(strings.TrimSuffix(filepath.Base(exe), ".cmd"), release.Command) {
				name := release.Command + ".exe"
				if release.Command == "codex" {
					name = "codex.js"
				}
				entry = filepath.Join(filepath.Dir(exe), "node_modules", filepath.FromSlash(release.Package), "bin", name)
				break
			}
		}
		if entry == "" {
			return nil, errors.New("unsupported command shim; use a native executable")
		}
		if _, err := os.Stat(entry); err != nil {
			return nil, errors.New("vendor entrypoint is absent; repair the harness installation")
		}
		exe = entry
	}
	if strings.HasSuffix(strings.ToLower(exe), ".bat") {
		return nil, errors.New("batch harness launchers require manual setup")
	}
	if strings.HasSuffix(exe, ".js") {
		node, err := exec.LookPath("node")
		if err != nil {
			return nil, errors.New("Node.js is required by the vendor launcher")
		}
		return exec.CommandContext(ctx, node, append([]string{exe}, args...)...), nil
	}
	return exec.CommandContext(ctx, exe, args...), nil
}

func detectHarness(harness string, install bool) (string, string, error) {
	release, ok := harnessReleases[harness]
	if !ok {
		return "", "", errors.New("unsupported harness")
	}
	exe, err := exec.LookPath(release.Command)
	if err != nil && install {
		npm, findErr := exec.LookPath("npm")
		if findErr != nil {
			return "", "", errors.New("automatic harness installation needs a supported Node.js/npm installation")
		}
		fmt.Printf("Install vendor package: npm install --global --registry=https://registry.npmjs.org/ %s@%s\n", release.Package, release.Version)
		fmt.Println("This runs the vendor's npm installation scripts. No elevated privileges are requested.")
		answer, askErr := ask("Install this pinned vendor release? yes/no", "no")
		if askErr != nil {
			return "", "", askErr
		}
		if answer != "yes" {
			return "", "", errors.New("vendor installation declined")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		cmd := externalCommand(ctx, npm, "install", "--global", "--registry=https://registry.npmjs.org/", release.Package+"@"+release.Version)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if cmd.Run() != nil {
			return "", "", errors.New("vendor installation failed; resolve npm permissions/network and retry")
		}
		exe, err = exec.LookPath(release.Command)
	}
	if err != nil {
		return "", "", errors.New("harness missing; use --install-missing or its official installation guide")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var output bytes.Buffer
	cmd, commandErr := harnessCommand(ctx, exe, "--version")
	if commandErr != nil {
		return "", "", commandErr
	}
	cmd.Stdout = &limitedWriter{Writer: &output, Remaining: 4096}
	if cmd.Run() != nil {
		return "", "", errors.New("harness version command failed")
	}
	if !reviewedVersionOutput(harness, output.String()) {
		return "", "", fmt.Errorf("installed %s version output is outside the reviewed installer allowlist (%s); inspect --version manually and use its manual guide", harness, release.Version)
	}
	return exe, release.Version, nil
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
