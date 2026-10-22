package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// Fixtures represent normal-user-owned config files even on elevated CI where
// Windows' default owner is the Administrators group.
func writeOwnedTestFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_RDWR, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = secureFile(f); err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}

func TestJSONPreservesUnrelatedSettings(t *testing.T) {
	before := []byte(`{"permissions":{"allow":["Read"]},"env":{"KEEP":"yes"},"counter":9007199254740993}`)
	out, err := render("claude", "claude-example", before)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(`9007199254740993`)) {
		t.Fatal("integer precision lost")
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["permissions"] == nil || doc["env"].(map[string]any)["KEEP"] != "yes" {
		t.Fatal("unrelated configuration lost")
	}
	if bytes.Contains(out, []byte("rk_live_")) {
		t.Fatal("unexpected secret")
	}
}

func TestTOMLPreservesOtherProviders(t *testing.T) {
	before := []byte("model = 'old'\n[model_providers.other]\nname = 'Other'\nbase_url = 'https://example.com/v1'\n")
	out, err := render("codex", "model-example", before)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	providers := doc["model_providers"].(map[string]any)
	if providers["other"] == nil || providers["phasesai"] == nil {
		t.Fatal("providers not preserved")
	}
	if doc["model_provider"] != "phasesai" {
		t.Fatal("provider not selected")
	}
}

func TestInvalidConfigRefused(t *testing.T) {
	for _, data := range []string{`null`, `{`, `{"env":false}`, `{} {}`, "// comment\n{}"} {
		if _, err := render("claude", "example", []byte(data)); err == nil {
			t.Errorf("accepted invalid or unsupported config %q", data)
		}
	}
}

func TestOpenCodePreservesModelsAndPersistsCredentials(t *testing.T) {
	out, err := renderPersistent("opencode", "claude-opus-5-5", "rk_live_fake_test", []byte(`{"provider":{"phasesai-anthropic":{"models":{"old-model":{"name":"Old"}}},"other":{"name":"Other"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("old-model")) || !bytes.Contains(out, []byte("rk_live_fake_test")) || bytes.Contains(out, []byte("{env:")) {
		t.Fatal("model lost or persistent credential missing")
	}
}

func TestBackupAndApply(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	old := []byte("old")
	next := []byte("new")
	if err := writeOwnedTestFile(file, old, 0600); err != nil {
		t.Fatal(err)
	}
	backup, err := transact(file, old, next, true, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(backup)
	if err != nil || !bytes.Equal(data, old) {
		t.Fatal("backup missing or wrong")
	}
	data, _ = os.ReadFile(file)
	if !bytes.Equal(data, next) {
		t.Fatal("new config not applied")
	}
}

func TestRollbackRestoresPreviousBytes(t *testing.T) {
	for _, existed := range []bool{true, false} {
		file := filepath.Join(t.TempDir(), "config")
		var old []byte
		if existed {
			old = []byte("previous")
			if err := writeOwnedTestFile(file, old, 0600); err != nil {
				t.Fatal(err)
			}
		}
		_, err := transact(file, old, []byte("next"), existed, func() error { return errors.New("failure") })
		if err == nil {
			t.Fatal("verification failure ignored")
		}
		data, readErr := os.ReadFile(file)
		if existed && (readErr != nil || !bytes.Equal(data, old)) {
			t.Fatal("previous bytes not restored")
		}
		if !existed && !os.IsNotExist(readErr) {
			t.Fatal("new file not removed")
		}
	}
}

func TestConcurrentChangeIsNotOverwritten(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config")
	if err := writeOwnedTestFile(file, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := transact(file, []byte("old"), []byte("next"), true, func() error { return nil }); err == nil {
		t.Fatal("stale preview accepted")
	}
	_, err := transact(file, []byte("external"), []byte("next"), true, func() error {
		if err := writeOwnedTestFile(file, []byte("edited"), 0600); err != nil {
			return err
		}
		return errors.New("failed check")
	})
	if err == nil {
		t.Fatal("check failure ignored")
	}
	data, _ := os.ReadFile(file)
	if string(data) != "edited" {
		t.Fatal("rollback overwrote external edit")
	}
}

func TestPathOutsideHomeRefused(t *testing.T) {
	home := t.TempDir()
	if err := safePath(home, filepath.Join(filepath.Dir(home), "elsewhere", "config")); err == nil {
		t.Fatal("outside path allowed")
	}
	if err := safePath(home, filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Fatal(err)
	}
}
