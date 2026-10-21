// Core skeleton: inspect README.md before using. No telemetry or self-update.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/term"
)

var version = "0.0.0-dev"

const origin = "https://api.phasesai.cc"

var input = bufio.NewReader(os.Stdin)

func ask(label, fallback string) (string, error) {
	fmt.Printf("%s [%s]: ", label, fallback)
	s, err := input.ReadString('\n')
	if err != nil {
		return "", errors.New("interactive input ended")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		s = fallback
	}
	return s, nil
}

func object(parent map[string]any, key string) (map[string]any, error) {
	if v, ok := parent[key]; ok {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", key)
		}
		return m, nil
	}
	m := map[string]any{}
	parent[key] = m
	return m, nil
}

// Merge semantically, not by string replacement. JSONC and formatting retention
// need separate production adapters; invalid input always fails before writing.
func render(harness, model string, previous []byte) ([]byte, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(previous)) > 0 {
		var err error
		if harness == "codex" {
			err = toml.Unmarshal(previous, &root)
		} else {
			d := json.NewDecoder(bytes.NewReader(previous))
			d.UseNumber()
			err = d.Decode(&root)
			if err == nil {
				var extra any
				if d.Decode(&extra) != io.EOF {
					err = errors.New("trailing JSON data")
				}
			}
		}
		if err != nil || root == nil {
			return nil, errors.New("cannot parse existing config; no changes made")
		}
	}
	switch harness {
	case "claude":
		env, err := object(root, "env")
		if err != nil {
			return nil, err
		}
		// Secrets stay in environment variables; never interpolate shell variables in JSON.
		env["ANTHROPIC_BASE_URL"] = origin
		env["ANTHROPIC_MODEL"] = model
	case "codex":
		providers, err := object(root, "model_providers")
		if err != nil {
			return nil, err
		}
		provider, err := object(providers, "phasesai")
		if err != nil {
			return nil, err
		}
		provider["name"] = "PhasesAI"
		provider["base_url"] = origin + "/v1"
		provider["env_key"] = "PHASESAI_API_KEY"
		provider["wire_api"] = "responses"
		provider["requires_openai_auth"] = false
		provider["supports_websockets"] = false
		root["model_provider"] = "phasesai"
		root["model"] = model
		return toml.Marshal(root)
	case "opencode":
		providers, err := object(root, "provider")
		if err != nil {
			return nil, err
		}
		provider, err := object(providers, "phasesai")
		if err != nil {
			return nil, err
		}
		provider["npm"] = "@ai-sdk/openai"
		provider["name"] = "PhasesAI"
		opts, err := object(provider, "options")
		if err != nil {
			return nil, err
		}
		opts["baseURL"] = origin + "/v1"
		opts["apiKey"] = "{env:PHASESAI_API_KEY}"
		models, err := object(provider, "models")
		if err != nil {
			return nil, err
		}
		entry, err := object(models, model)
		if err != nil {
			return nil, err
		}
		entry["name"] = model
		root["model"] = "phasesai/" + model
	default:
		return nil, errors.New("unsupported harness")
	}
	out, err := json.MarshalIndent(root, "", "  ")
	return append(out, '\n'), err
}

// Restrict writes to a user-selected path inside home; reject links along it.
func safePath(home, target string) error {
	rel, err := filepath.Rel(home, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("config must be inside your home directory")
	}
	cur := home
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink config paths require manual setup")
		}
	}
	return nil
}

// One config per transaction. Backups remain after successful or failed checks.
// Production work: persistent crash journal, ACLs, OS lock, and fsync directory.
func transact(path string, before, after []byte, existed bool, verify func() error) (string, error) {
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if (err == nil) != existed || !bytes.Equal(current, before) {
		return "", errors.New("config changed since preview; retry")
	}
	if bytes.Equal(before, after) {
		return "", verify()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	backup := ""
	if existed {
		backup = fmt.Sprintf("%s.phasesai-%d.bak", path, time.Now().UnixNano())
		f, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return "", err
		}
		_, writeErr := f.Write(before)
		syncErr := f.Sync()
		closeErr := f.Close()
		if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
			return backup, err
		}
	}
	write := func(data []byte) error {
		f, err := os.CreateTemp(filepath.Dir(path), ".phasesai-stage-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if err = f.Chmod(0600); err != nil {
			f.Close()
			return err
		}
		_, writeErr := f.Write(data)
		syncErr := f.Sync()
		closeErr := f.Close()
		if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
			return err
		}
		return os.Rename(f.Name(), path)
	}
	if err = write(after); err != nil {
		return backup, err
	}
	if err = verify(); err == nil {
		return backup, nil
	}
	// Do not roll back over edits made by another process after installation.
	installed, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(installed, after) {
		return backup, errors.New("verification failed; config changed; restore backup manually")
	}
	var rollbackErr error
	if existed {
		rollbackErr = write(before)
	} else {
		rollbackErr = os.Remove(path)
	}
	if rollbackErr != nil {
		return backup, errors.New("verification failed; rollback failed; restore backup manually")
	}
	return backup, errors.New("verification failed; previous config restored")
}

// Fixed HTTPS origin, no redirects, bounded body and timeout; keys cannot leak to
// another host via a redirect or custom endpoint. No response body is logged.
func request(key, method, endpoint string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, origin+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if endpoint == "/v1/messages" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("network request failed; check connectivity and TLS")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, errors.New("could not read response")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("gateway returned HTTP %d", res.StatusCode)
	}
	return data, nil
}

func run() error {
	harness := flag.String("harness", "", "claude, codex, or opencode (v1)")
	model := flag.String("model", "", "exact model ID from the live catalog")
	config := flag.String("config", "", "config path inside home")
	dry := flag.Bool("dry-run", false, "preview without changing files")
	smoke := flag.Bool("smoke-test", false, "offer a billable inference check before writing")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if flag.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("run interactively in a terminal; do not pipe keys to stdin")
	}
	if runtime.GOOS == "windows" && !*dry {
		return errors.New("TODO: verify Windows ACLs, reparse-point handling, and atomic replacement; skeleton supports Windows dry-run only")
	}
	var err error
	if *harness == "" {
		*harness, err = ask("Harness: claude / codex / opencode", "codex")
		if err != nil {
			return err
		}
	}
	command := map[string]string{"claude": "claude", "codex": "codex", "opencode": "opencode"}[*harness]
	if command == "" {
		return errors.New("unsupported harness; use a documented manual guide")
	}
	exe, err := exec.LookPath(command)
	if err != nil {
		return errors.New("harness not installed; follow the manual installation guide first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, exe, "--version").Output()
	if err != nil {
		return errors.New("could not detect harness version")
	}
	if *harness == "opencode" && !regexp.MustCompile(`(?m)^(?:opencode\s+)?1\.`).Match(bytes.TrimSpace(output)) {
		return errors.New("this skeleton targets OpenCode 1; use manual setup for other versions")
	}
	fmt.Printf("Detected %s. Version output suppressed; check --version manually.\n", command)
	key := os.Getenv("PHASESAI_API_KEY")
	if key == "" {
		fmt.Print("PhasesAI API key (hidden, verification only; not saved): ")
		secret, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return errors.New("could not read secret")
		}
		key = string(secret)
		clear(secret)
	}
	if strings.TrimSpace(key) != key || len(key) < 8 || strings.ContainsAny(key, "\r\n\x00") {
		return errors.New("key format invalid")
	}
	catalog, err := request("", "GET", "/v1/models?limit=1000", nil)
	if err != nil {
		return err
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		HasMore bool `json:"has_more"`
	}
	if json.Unmarshal(catalog, &list) != nil || len(list.Data) == 0 {
		return errors.New("no usable catalog")
	}
	if list.HasMore {
		return errors.New("TODO: implement complete catalog pagination; no changes made")
	}
	if *model == "" {
		fmt.Println("Choose an exact model ID from https://api.phasesai.cc/v1/models")
		*model, err = ask("Model ID", "")
		if err != nil {
			return err
		}
	}
	found := false
	for _, m := range list.Data {
		if m.ID == *model {
			found = true
		}
	}
	if !found {
		return errors.New("selected model is absent from the catalog")
	}
	if *harness == "claude" && !strings.HasPrefix(*model, "claude-") {
		return errors.New("Claude Code recipe requires a Claude model")
	}
	if strings.HasPrefix(*model, "gpt-image-") {
		return errors.New("select a text model for coding")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	defaultPath := filepath.Join(home, ".claude", "settings.json")
	if *harness == "codex" {
		base := os.Getenv("CODEX_HOME")
		if base == "" {
			base = filepath.Join(home, ".codex")
		}
		defaultPath = filepath.Join(base, "config.toml")
	}
	if *harness == "opencode" {
		defaultPath = filepath.Join(home, ".config", "opencode", "opencode.json")
		if p := os.Getenv("XDG_CONFIG_HOME"); p != "" {
			defaultPath = filepath.Join(p, "opencode", "opencode.json")
		}
		if p := os.Getenv("OPENCODE_CONFIG"); p != "" {
			defaultPath = p
		}
	}
	if *config == "" {
		*config, err = ask("Config file", defaultPath)
		if err != nil {
			return err
		}
	}
	target, err := filepath.Abs(*config)
	if err != nil {
		return err
	}
	if err = safePath(home, target); err != nil {
		return err
	}
	before, readErr := os.ReadFile(target)
	existed := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return errors.New("cannot read config")
	}
	// Don't unexpectedly create an alternate JSON config beside existing JSONC.
	if *harness == "opencode" && !existed {
		if _, err := os.Stat(strings.TrimSuffix(target, ".json") + ".jsonc"); err == nil {
			return errors.New("JSONC config detected; manual merge required")
		}
	}
	after, err := render(*harness, *model, before)
	if err != nil {
		return err
	}
	fmt.Printf("Plan: update %s\nConfig path: %s\nAPI: %s\n", command, target, origin)
	fmt.Println("Existing unrelated values retained; JSON/TOML comments and formatting may change.")
	fmt.Println("API key is never saved. Future launches need PHASESAI_API_KEY; Claude also needs ANTHROPIC_AUTH_TOKEN.")
	fmt.Printf("Before SHA-256: %x\nAfter SHA-256: %x\n", sha256.Sum256(before), sha256.Sum256(after))
	if *dry {
		fmt.Println("Dry run: no files changed; key and balance are not validated by public discovery.")
		return nil
	}
	if *smoke {
		answer, err := ask("Allow one small inference request that may be billed? yes/no", "no")
		if err != nil {
			return err
		}
		if answer != "yes" {
			return errors.New("inference check declined; no changes made")
		}
		payload := map[string]any{"model": *model, "input": "Reply with OK.", "store": false}
		endpoint := "/v1/responses"
		if *harness == "claude" {
			endpoint = "/v1/messages"
			payload = map[string]any{"model": *model, "max_tokens": 32, "messages": []any{map[string]any{"role": "user", "content": "Reply with OK."}}}
		}
		body, _ := json.Marshal(payload)
		data, err := request(key, "POST", endpoint, body)
		if err != nil {
			return err
		}
		var reply map[string]any
		if json.Unmarshal(data, &reply) != nil || reply["error"] != nil {
			return errors.New("inference check did not return a valid success object")
		}
		if *harness == "claude" && reply["type"] != "message" || *harness != "claude" && reply["status"] != "completed" {
			return errors.New("inference check did not complete")
		}
		fmt.Println("Inference completed; verify usage in dashboard.")
	} else {
		fmt.Println("Key validity, balance, and inference compatibility have not been checked.")
	}
	answer, err := ask("Back up and apply this config? yes/no", "no")
	if err != nil {
		return err
	}
	if answer != "yes" {
		return errors.New("cancelled; no changes made")
	}
	backup, err := transact(target, before, after, existed, func() error {
		written, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		if !bytes.Equal(written, after) {
			return errors.New("written bytes differ")
		}
		_, err = render(*harness, *model, written)
		return err
	})
	if backup != "" {
		fmt.Println("Backup:", backup)
	}
	if err != nil {
		return err
	}
	fmt.Println("Config file verified. Complete the harness verification steps in the docs.")
	if *harness == "claude" {
		fmt.Println("POSIX launch shell: export ANTHROPIC_AUTH_TOKEN=\"$PHASESAI_API_KEY\"")
	}
	fmt.Println("Existing auth files, shell profiles, and other providers were not removed.")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Setup:", err)
		os.Exit(1)
	}
}
