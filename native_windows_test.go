//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in live acceptance: real latest binaries, dummy credentials, isolated
// profile, and only built-in Windows tools on PATH. Never sends inference.
func TestNativeWindowsCleanProfile(t *testing.T) {
	if os.Getenv("PHASESAI_WINDOWS_ACCEPTANCE") != "1" {
		t.Skip("enable live Windows download acceptance explicitly")
	}
	home := t.TempDir()
	for name, value := range map[string]string{
		"USERPROFILE": home, "HOME": home, "LOCALAPPDATA": filepath.Join(home, "AppData", "Local"), "APPDATA": filepath.Join(home, "AppData", "Roaming"),
		"CODEX_HOME": "", "CLAUDE_CONFIG_DIR": "", "OPENCODE_CONFIG": "", "OPENCODE_CONFIG_CONTENT": "", "OPENCODE_CONFIG_DIR": "",
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_DATA_HOME": filepath.Join(home, ".local", "share"), "XDG_STATE_HOME": filepath.Join(home, ".local", "state"), "XDG_CACHE_HOME": filepath.Join(home, ".cache"),
		"PHASESAI_API_KEY": "", "OPENAI_API_KEY": "", "ANTHROPIC_AUTH_TOKEN": "", "ANTHROPIC_API_KEY": "", "CLAUDE_CODE_OAUTH_TOKEN": "",
		"PATH": filepath.Join(os.Getenv("SystemRoot"), "System32") + ";" + filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0"),
	} {
		t.Setenv(name, value)
		if value == "" {
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	key := "rk_live_acceptance_dummy_never_sent"
	for _, h := range []string{"claude", "codex", "opencode"} {
		t.Run(h, func(t *testing.T) {
			r, err := latestRelease(h, windowsArchitecture())
			if err != nil {
				t.Fatal(err)
			}
			exe, err := installNative(home, h, r)
			if err != nil {
				t.Fatal(err)
			}
			t.Log(h, r.Version, "checksum and native version passed with no Node/npm/Git on PATH")
			m := "claude-opus-5-5"
			if h == "codex" {
				m = "gpt-6.1-sol"
			}
			after, err := renderPersistent(h, m, key, nil)
			if err != nil {
				t.Fatal(err)
			}
			path := defaultConfig(home, h)
			p, err := planFile(home, path, after)
			if err != nil {
				t.Fatal(err)
			}
			plans := []configPlan{p}
			if h == "codex" {
				for _, name := range []string{"codex-package.json", "codex-path/rg.exe", "codex-resources/codex-command-runner.exe", "codex-resources/codex-windows-sandbox-setup.exe"} {
					if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(exe)), filepath.FromSlash(name))); err != nil {
						t.Fatal("incomplete runtime", name, err)
					}
				}
				auth, err := codexAuth(home, exe, key)
				if err != nil {
					t.Fatal(err)
				}
				p, err := planFile(home, filepath.Join(filepath.Dir(path), "auth.json"), auth)
				if err != nil {
					t.Fatal(err)
				}
				plans = append(plans, p)
			}
			if err := applyPlans(home, plans); err != nil {
				t.Fatal(err)
			}
			{
				args := []string{"login", "status"}
				if h == "claude" {
					args = []string{"auth", "status"}
				}
				if h == "opencode" {
					args = []string{"debug", "config"}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				cmd, err := harnessCommand(ctx, exe, args...)
				if err != nil {
					t.Fatal(err)
				}
				cmd.Dir = home
				if h == "codex" {
					cmd.Env = append(os.Environ(), "CODEX_HOME="+filepath.Dir(path))
				}
				var out bytes.Buffer
				cmd.Stdout = &limitedWriter{Writer: &out, Remaining: 2 << 20}
				cmd.Stderr = io.Discard
				if h == "codex" {
					cmd.Stderr = cmd.Stdout
				}
				if err := cmd.Run(); err != nil {
					t.Fatal("native persisted config validation failed (output withheld)", err)
				}
				if h == "codex" && !strings.Contains(strings.ToLower(out.String()), "api key") {
					t.Fatal("Codex did not select persisted API-key auth (output withheld)")
				}
				if h == "claude" {
					var status map[string]any
					if json.Unmarshal(out.Bytes(), &status) != nil || status["loggedIn"] != true {
						t.Fatal("Claude did not recognize its saved token (output withheld)")
					}
				}
				if h == "opencode" {
					var config map[string]any
					data := out.Bytes()
					start := bytes.IndexByte(data, '{')
					end := bytes.LastIndexByte(data, '}')
					if start < 0 || end < start || json.Unmarshal(data[start:end+1], &config) != nil {
						t.Fatal("OpenCode did not return effective config (output withheld)")
					}
					for _, provider := range []string{"phasesai-anthropic", "phasesai-gpt"} {
						got, _ := fieldValue(config, []string{"provider", provider, "options", "apiKey"})
						// OpenCode intentionally redacts secrets in debug config.
						if got != "***" && got != key {
							t.Fatal("OpenCode did not recognize saved provider credentials")
						}
						base, _ := fieldValue(config, []string{"provider", provider, "options", "baseURL"})
						if base != origin+"/v1" {
							t.Fatal("OpenCode provider endpoint mismatch")
						}
					}
				}
			}
		})
	}
}
