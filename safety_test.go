package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestDryRunRejectsRecoveryAndLaunchBeforeSideEffects(t *testing.T) {
	previousArgs, previousFlags := os.Args, flag.CommandLine
	defer func() { os.Args, flag.CommandLine = previousArgs, previousFlags }()
	for _, args := range [][]string{{"--dry-run", "--recover", "--config", "unused"}, {"--dry-run", "--launch", "codex"}} {
		os.Args = append([]string{"phasesai-setup"}, args...)
		flag.CommandLine = flag.NewFlagSet("phasesai-setup", flag.ContinueOnError)
		err := run()
		if err == nil || !strings.Contains(err.Error(), "--dry-run cannot") {
			t.Fatalf("unsafe mode combination reached an action: %v", err)
		}
	}
}

func TestStableVersionOutputRefusesPrereleasesAndExtraText(t *testing.T) {
	for harness, value := range map[string]string{"claude": "2.1.306 (Claude Code)", "codex": "codex-cli 0.163.0", "opencode": "1.19.3"} {
		if _, err := versionOutput(harness, value+"\n"); err != nil {
			t.Fatal("known version rejected")
		}
		for _, bad := range []string{value + "-beta", "prefix " + value, value + "\nunexpected", "unexpected"} {
			if _, err := versionOutput(harness, bad); err == nil {
				t.Fatal("malformed version output accepted")
			}
		}
	}
}

func TestJSONCPreservesCommentsAndTrailingComma(t *testing.T) {
	before := []byte("{\n// Keep my note\n\"theme\":\"blue\",\n\"provider\": {\"other\": {\"name\":\"Existing\"}},\n}\n")
	original := bytes.Clone(before)
	out, err := render("opencode", "text-model", before)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("// Keep my note")) || !bytes.Contains(out, []byte("Existing")) {
		t.Fatalf("comments or other providers lost: %s", out)
	}
	if !bytes.Equal(before, original) {
		t.Fatal("input/backup bytes were mutated")
	}
	again, err := render("opencode", "text-model", out)
	if err != nil || !bytes.Equal(again, out) {
		t.Fatal("render is not idempotent")
	}
}

func TestTOMLPatchesOnlyOwnedScalarRanges(t *testing.T) {
	before := []byte("# My top note\nmodel = 'old' # chosen model\nkeep = [1, 2] # preserve spacing\n[model_providers.other]\nbase_url = 'https://example.com'\n[model_providers.phasesai]\nname = 'Old label' # retain this note\nbase_url = 'https://old.example/v1'\nwire_api = 'responses'\nrequires_openai_auth = true\n[features]\nkeep_feature = true\n")
	out, err := render("codex", "new-model", before)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"# My top note", "# chosen model", "keep = [1, 2] # preserve spacing", "base_url = 'https://example.com'", "# retain this note", "[features]\nkeep_feature = true"} {
		if !bytes.Contains(out, []byte(expected)) {
			t.Fatalf("lost unrelated content %q", expected)
		}
	}
	root, err := decodeConfig("codex", out)
	if err != nil {
		t.Fatal(err)
	}
	value, _ := fieldValue(root, []string{"model_providers", "phasesai", "requires_openai_auth"})
	if value != true {
		t.Fatal("boolean patch failed")
	}
	again, err := render("codex", "new-model", out)
	if err != nil || !bytes.Equal(out, again) {
		t.Fatal("TOML is not idempotent")
	}
}

func TestDuplicateJSONAndCredentialConflictsRefused(t *testing.T) {
	if _, err := render("opencode", "m", []byte(`{"model":"a","model":"b"}`)); err == nil {
		t.Fatal("duplicate keys accepted")
	}
	for _, fixture := range []struct{ harness, config string }{
		{"claude", `{"env":{"ANTHROPIC_API_KEY":"old-secret"}}`},
		{"codex", "[model_providers.phasesai]\nhttp_headers={Authorization='secret'}\n"},
		{"opencode", `{"provider":{"phasesai":{"options":{"headers":{"authorization":"secret"}}}}}`},
	} {
		paths, err := configConflicts(fixture.harness, []byte(fixture.config))
		if err != nil || len(paths) == 0 {
			t.Fatalf("conflict not found: %v", err)
		}
		if strings.Contains(strings.Join(paths, ","), "secret") {
			t.Fatal("secret leaked through conflict diagnostics")
		}
	}
}

func TestNativeLockExcludesAnotherInstaller(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "settings.json")
	first, err := openStore(home, target)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := openStore(home, target); err == nil {
		second.close()
		t.Fatal("concurrent lock acquired")
	}
	first.close()
	second, err := openStore(home, target)
	if err != nil {
		t.Fatal("lock not released", err)
	}
	second.close()
}

func TestRecoveryAfterInterruptedInstall(t *testing.T) {
	for _, existed := range []bool{true, false} {
		for _, applied := range []bool{true, false} {
			home := t.TempDir()
			target := filepath.Join(home, "settings.json")
			old := []byte(nil)
			if existed {
				old = []byte("old config")
				if err := writeOwnedTestFile(target, old, 0600); err != nil {
					t.Fatal(err)
				}
			}
			s, err := openStore(home, target)
			if err != nil {
				t.Fatal(err)
			}
			_, _, metadata, err := s.read(s.name)
			if err != nil {
				t.Fatal(err)
			}
			backup := ""
			if existed {
				backup = s.name + ".phasesai-test.bak"
				if err = s.exclusive(backup, old, nil); err != nil {
					t.Fatal(err)
				}
			}
			stage := s.name + ".phasesai-test.stage"
			next := []byte("new config")
			if err = s.exclusive(stage, next, nil); err != nil {
				t.Fatal(err)
			}
			j := transactionJournal{1, digest(old), digest(next), existed, backup, stage, metadata}
			raw, _ := json.Marshal(j)
			if err = s.exclusive(s.journalName(), raw, nil); err != nil {
				t.Fatal(err)
			}
			if applied {
				if err = s.parent.Rename(stage, s.name); err != nil {
					t.Fatal(err)
				}
			}
			s.close()
			if err = recoverTransaction(home, target); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(target)
			if existed && (err != nil || !bytes.Equal(data, old)) {
				t.Fatal("original not recovered")
			}
			if !existed && !os.IsNotExist(err) {
				t.Fatal("new config remains")
			}
			if _, err = os.Stat(target + ".phasesai-journal.json"); !os.IsNotExist(err) {
				t.Fatal("journal remains")
			}
		}
	}
}

func TestRecoveryDoesNotClobberExternalEdits(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "config")
	before := []byte("before")
	after := []byte("after")
	writeOwnedTestFile(target, before, 0600)
	_, err := transactHome(home, target, before, after, true, func() error {
		if err := writeOwnedTestFile(target, []byte("external edit"), 0600); err != nil {
			t.Fatal(err)
		}
		return errors.New("failed")
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if err = recoverTransaction(home, target); err == nil {
		t.Fatal("external edit overwritten")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "external edit" {
		t.Fatal("external edit lost")
	}
}

func TestLinkedConfigRefused(t *testing.T) {
	home := t.TempDir()
	outside := filepath.Join(t.TempDir(), "target")
	writeOwnedTestFile(outside, []byte("keep"), 0600)
	target := filepath.Join(home, "config")
	if err := os.Link(outside, target); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readPreview(home, target); err == nil {
		t.Fatal("hard-linked config accepted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockGateway(t *testing.T, fn roundTripFunc) {
	t.Helper()
	old := gatewayHTTP
	gatewayHTTP = &http.Client{Transport: fn, CheckRedirect: old.CheckRedirect}
	t.Cleanup(func() { gatewayHTTP = old })
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestCatalogPaginationAndCursorCycle(t *testing.T) {
	calls := 0
	mockGateway(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "" {
			t.Fatal("key sent to public discovery")
		}
		if calls == 1 {
			return reply(200, `{"data":[{"id":"a / b"}],"has_more":true,"last_id":"a / b"}`), nil
		}
		if r.URL.Query().Get("after_id") != "a / b" {
			t.Fatal("cursor not encoded")
		}
		return reply(200, `{"data":[{"id":"c"}],"has_more":false}`), nil
	})
	models, err := fetchCatalog()
	if err != nil || len(models) != 2 || calls != 2 {
		t.Fatal("pagination failed", err)
	}
	mockGateway(t, func(r *http.Request) (*http.Response, error) {
		return reply(200, `{"data":[{"id":"a"}],"has_more":true,"last_id":"a"}`), nil
	})
	if _, err = fetchCatalog(); err == nil {
		t.Fatal("cursor loop not detected")
	}
}

func TestRedirectsAndBillableRetriesAreRefused(t *testing.T) {
	calls := 0
	mockGateway(t, func(r *http.Request) (*http.Response, error) {
		calls++
		res := reply(302, "sensitive body")
		res.Header.Set("Location", "https://attacker.invalid/")
		return res, nil
	})
	if _, err := request("secret", "POST", "/v1/responses", []byte(`{}`)); err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("redirect handling or body redaction failed")
	}
	if calls != 1 {
		t.Fatal("billable request repeated")
	}
}

func TestSavedKeyUsesOSAdapterAndNeverConfig(t *testing.T) {
	keyring.MockInit()
	if err := saveKey("codex", "fake-key-for-test"); err != nil {
		t.Fatal(err)
	}
	key, err := savedKey("codex")
	if err != nil || key != "fake-key-for-test" {
		t.Fatal("credential adapter failed")
	}
	out, err := render("codex", "m", nil)
	if err != nil || bytes.Contains(out, []byte(key)) {
		t.Fatal("secret serialized in config")
	}
}

func TestAuditUsesOnlySafeFields(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "logs", "setup.jsonl")
	audit, err := openAudit(home, path)
	if err != nil {
		t.Fatal(err)
	}
	defer audit.close()
	if err = audit.event("start", "ok", "codex"); err != nil {
		t.Fatal(err)
	}
	if err = audit.event("secret-example", "ok", "codex"); err == nil {
		t.Fatal("arbitrary data accepted as event")
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), home) {
		t.Fatal("private path leaked or log missing")
	}
}
