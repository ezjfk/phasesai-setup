package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

func defaultConfig(home, harness string) string {
	switch harness {
	case "claude":
		base := os.Getenv("CLAUDE_CONFIG_DIR")
		if base == "" {
			base = filepath.Join(home, ".claude")
		}
		return filepath.Join(base, "settings.json")
	case "codex":
		base := os.Getenv("CODEX_HOME")
		if base == "" {
			base = filepath.Join(home, ".codex")
		}
		return filepath.Join(base, "config.toml")
	case "opencode":
		if p := os.Getenv("OPENCODE_CONFIG"); p != "" {
			return p
		}
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		p := filepath.Join(base, "opencode", "opencode.jsonc")
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if _, err = os.Stat(p[:len(p)-1]); err == nil {
				return p[:len(p)-1]
			}
		}
		return p
	}
	return ""
}

func readPreview(home, target string) ([]byte, bool, error) {
	rel, err := localTarget(home, target)
	if err != nil {
		return nil, false, err
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	f, err := root.Open(rel)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.New("cannot read config")
	}
	defer f.Close()
	if _, err = inspectFile(f); err != nil {
		return nil, false, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil || len(data) > maxConfigBytes {
		return nil, false, errors.New("config unreadable or exceeds 2 MiB")
	}
	return data, true, nil
}
