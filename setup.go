package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type configPlan struct {
	Path          string
	Before, After []byte
	Existed       bool
}

func planFile(home, path string, after []byte) (configPlan, error) {
	if err := safePath(home, path); err != nil {
		return configPlan{}, err
	}
	before, existed, err := readPreview(home, path)
	return configPlan{path, before, after, existed}, err
}

func applyPlans(home string, plans []configPlan) error {
	var applied []configPlan
	for _, p := range plans {
		backup, err := transactPrivateHome(home, p.Path, p.Before, p.After, p.Existed, func() error {
			actual, _, err := readPreview(home, p.Path)
			if err != nil {
				return err
			}
			if !bytes.Equal(actual, p.After) {
				return errors.New("saved config verification failed")
			}
			return nil
		})
		if backup != "" {
			fmt.Println("      Backup:", backup)
		}
		if err != nil {
			var rollbackErr error
			for i := len(applied) - 1; i >= 0; i-- {
				if e := rollbackPlan(home, applied[i]); e != nil {
					rollbackErr = e
				}
			}
			if rollbackErr != nil {
				return errors.New("configuration failed; some files changed concurrently and could not be restored; review the printed backups")
			}
			return err
		}
		applied = append(applied, p)
	}
	return nil
}

func rollbackPlan(home string, p configPlan) error {
	if p.Existed {
		_, err := transactHome(home, p.Path, p.After, p.Before, true, func() error { return nil })
		return err
	}
	s, err := openStore(home, p.Path)
	if err != nil {
		return err
	}
	defer s.close()
	data, _, _, err := s.read(s.name)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, p.After) {
		return errors.New("config changed after setup")
	}
	return s.parent.Remove(s.name)
}

// Ask Codex itself to serialize its supported login schema in a disposable home.
// The key goes only over stdin; output is discarded and no inference is sent.
func codexAuth(home, exe, key string) ([]byte, error) {
	root, err := installRoot(home)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(root, ".auth-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err := writePrivate(filepath.Join(stage, "config.toml"), []byte("cli_auth_credentials_store = \"file\"\n")); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd, err := harnessCommand(ctx, exe, "login", "--with-api-key")
	if err != nil {
		return nil, err
	}
	cmd.Env = []string{}
	for _, entry := range os.Environ() {
		name := strings.ToUpper(strings.SplitN(entry, "=", 2)[0])
		if name == "CODEX_HOME" || name == "OPENAI_API_KEY" || name == "CODEX_API_KEY" || name == "CODEX_ACCESS_TOKEN" || name == "CODEX_IDENTITY_TOKEN_FILE" || name == "CODEX_IDENTITY_TOKEN_AUDIENCE" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "CODEX_HOME="+stage)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = stage, strings.NewReader(key+"\n"), io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		return nil, errors.New("Codex could not save its API-key login; no user configuration was changed")
	}
	path := filepath.Join(stage, "auth.json")
	// Vendor-created file may inherit an administrative owner on elevated systems.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, errors.New("Codex did not generate auth.json")
	}
	if err := secureFile(f); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	data, _, err := readPreview(home, path)
	if err != nil {
		return nil, err
	}
	var auth map[string]any
	if parseJSON(data, &auth) != nil || auth["OPENAI_API_KEY"] != key {
		return nil, errors.New("Codex generated an unexpected authentication format; no user configuration was changed")
	}
	return data, nil
}

func writePrivate(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := secureFile(f); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func persistInstallation(home, harness, exe, v string) error {
	root, err := installRoot(home)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, exe)
	if err != nil || !filepath.IsLocal(rel) {
		return errors.New("invalid installed executable path")
	}
	data, _ := json.MarshalIndent(installation{v, rel}, "", "  ")
	p, err := planFile(home, filepath.Join(root, harness+"-installed.json"), append(data, '\n'))
	if err != nil {
		return err
	}
	return applyPlans(home, []configPlan{p})
}
