package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/term"
	"io"
	"os"
	"path/filepath"
	"strings"
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
		p := filepath.Join(base, "opencode", "opencode.json")
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if _, err = os.Stat(p + "c"); err == nil {
				return p + "c"
			}
		}
		return p
	}
	return ""
}

func inspectOverrides(home, harness string, before []byte) error {
	if harness == "opencode" {
		for _, name := range []string{"OPENCODE_CONFIG_CONTENT", "OPENCODE_CONFIG_DIR"} {
			if os.Getenv(name) != "" {
				return fmt.Errorf("%s selects additional configuration; review it manually before setup", name)
			}
		}
	}
	conflicts, err := configConflicts(harness, before)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("conflicting credential/header settings at %s; review them manually before setup", strings.Join(conflicts, ", "))
	}
	if harness == "claude" {
		for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
			if os.Getenv(name) != "" {
				return fmt.Errorf("%s is already set; use a clean setup shell and the documented launcher to avoid competing credentials", name)
			}
		}
		if value := os.Getenv("ANTHROPIC_BASE_URL"); value != "" && value != origin {
			return errors.New("ANTHROPIC_BASE_URL points elsewhere; review the launch environment first")
		}
	}
	if harness == "codex" {
		root, err := decodeConfig(harness, before)
		if err != nil {
			return err
		}
		if profiles, ok := root["profiles"].(map[string]any); ok {
			for _, profile := range profiles {
				if p, ok := profile.(map[string]any); ok {
					for _, key := range []string{"model", "model_provider"} {
						if _, exists := p[key]; exists {
							return errors.New("Codex profile model/provider overrides exist; configure and verify the selected profile manually")
						}
					}
				}
			}
		}
	}
	current, err := os.Getwd()
	if err != nil {
		return err
	}
	names := map[string][]string{"claude": {filepath.Join(".claude", "settings.json"), filepath.Join(".claude", "settings.local.json")}, "codex": {filepath.Join(".codex", "config.toml")}, "opencode": {"opencode.json", "opencode.jsonc", filepath.Join(".opencode", "opencode.json"), filepath.Join(".opencode", "opencode.jsonc")}}[harness]
	for {
		for _, name := range names {
			p := filepath.Join(current, name)
			if filepath.Clean(p) == filepath.Clean(defaultConfig(home, harness)) {
				continue
			}
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return errors.New("project configuration detected; run setup from a neutral directory, then verify effective settings in the project")
			}
		}
		if current == home || filepath.Dir(current) == current {
			break
		}
		current = filepath.Dir(current)
	}
	return nil
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

func run() (result error) {
	harness := flag.String("harness", "", "claude, codex, or opencode (v1)")
	model := flag.String("model", "", "exact text-model ID from the live catalog")
	config := flag.String("config", "", "config path inside home")
	dry := flag.Bool("dry-run", false, "preview without config, log or credential writes")
	smoke := flag.Bool("smoke-test", false, "offer one potentially billable inference check before writing")
	install := flag.Bool("install-missing", false, "offer installation of the pinned vendor npm package")
	save := flag.Bool("save-key", false, "store the key in the OS credential store after successful configuration")
	recover := flag.Bool("recover", false, "restore an interrupted transaction; requires --config")
	launch := flag.String("launch", "", "launch a configured harness using an environment or saved key; arguments follow --")
	logPath := flag.String("log-file", "", "opt-in private JSONL event log inside home (rotates at 1 MiB)")
	showVersion := flag.Bool("version", false, "print build version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if *launch != "" {
		return launchHarness(*launch, flag.Args())
	}
	if flag.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("run interactively in a terminal; do not pipe keys to stdin")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if *recover {
		if *config == "" {
			return errors.New("--recover requires the exact --config path")
		}
		target, err := filepath.Abs(*config)
		if err != nil {
			return err
		}
		answer, err := ask("Restore the interrupted transaction if the current file still matches it? yes/no", "no")
		if err != nil {
			return err
		}
		if answer != "yes" {
			return errors.New("recovery cancelled")
		}
		if err = recoverTransaction(home, target); err != nil {
			return err
		}
		fmt.Println("Interrupted transaction recovered. Backups retained.")
		return nil
	}
	if *harness == "" {
		*harness, err = ask("Harness: claude / codex / opencode", "codex")
		if err != nil {
			return err
		}
	}
	if _, ok := harnessReleases[*harness]; !ok {
		return errors.New("unsupported harness; use its documented manual guide")
	}
	if *dry && (*install || *save || *logPath != "") {
		return errors.New("--dry-run cannot install software, save credentials or create logs")
	}
	var audit *auditLog
	if *logPath != "" {
		audit, err = openAudit(home, *logPath)
		if err != nil {
			return errors.New("cannot open private audit log")
		}
		defer audit.close()
		if err = audit.event("start", "ok", *harness); err != nil {
			return err
		}
		defer func() {
			outcome := "ok"
			if result != nil {
				outcome = "failed"
			}
			_ = audit.event("complete", outcome, *harness)
		}()
	}
	_, installed, err := detectHarness(*harness, *install)
	if err != nil {
		return err
	}
	fmt.Printf("Detected %s %s (reviewed installer version; live compatibility is separate).\n", *harness, installed)
	selectedPath := *config
	if selectedPath == "" {
		selectedPath, err = ask("Config file", defaultConfig(home, *harness))
		if err != nil {
			return err
		}
	}
	if strings.HasPrefix(selectedPath, "~"+string(os.PathSeparator)) {
		selectedPath = filepath.Join(home, selectedPath[2:])
	}
	target, err := filepath.Abs(selectedPath)
	if err != nil {
		return err
	}
	if err = safePath(home, target); err != nil {
		return err
	}
	before, existed, err := readPreview(home, target)
	if err != nil {
		return err
	}
	if err = inspectOverrides(home, *harness, before); err != nil {
		return err
	}
	if _, err = decodeConfig(*harness, before); err != nil {
		return err
	}
	key := os.Getenv("PHASESAI_API_KEY")
	if key == "" {
		key, _ = savedKey(*harness)
	}
	if key == "" {
		fmt.Print("PhasesAI API key (hidden): ")
		secret, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return errors.New("could not read secret")
		}
		key = string(secret)
		clear(secret)
	}
	if strings.TrimSpace(key) != key || len(key) < 8 || len(key) > 4096 || strings.ContainsAny(key, "\r\n\x00") {
		return errors.New("key format invalid")
	}
	catalog, err := fetchCatalog()
	if err != nil {
		return err
	}
	if *model == "" {
		fmt.Println("Choose an exact text-model ID from https://api.phasesai.cc/v1/models")
		*model, err = ask("Model ID", "")
		if err != nil {
			return err
		}
	}
	var selected *catalogModel
	for _, entry := range catalog {
		if entry.ID == *model {
			copy := entry
			selected = &copy
		}
	}
	if selected == nil {
		return errors.New("selected model is absent from the complete catalog")
	}
	if *harness == "claude" && !strings.HasPrefix(*model, "claude-") {
		return errors.New("Claude Code requires a Claude model")
	}
	if strings.HasPrefix(*model, "gpt-image-") {
		return errors.New("select a text model for coding")
	}
	if selected.MaxTokens > 0 && selected.MaxTokens < 32 && *smoke {
		return errors.New("selected model's advertised output cap is below the 32-token smoke request")
	}
	fmt.Println("Catalog route hints do not certify streaming, tools or adapter compatibility. Verify this exact combination in your harness.")
	after, err := render(*harness, *model, before)
	if err != nil {
		return err
	}
	changes, err := changedFields(*harness, *model, before)
	if err != nil {
		return err
	}
	fmt.Printf("Plan: update %s\nConfig: %s\nAPI: %s\n", *harness, target, origin)
	fmt.Println("Changed field paths (values hidden):")
	for _, change := range changes {
		fmt.Println("  " + strings.Join(change.Path, "."))
	}
	fmt.Printf("Before SHA-256: %x\nAfter SHA-256: %x\n", sha256.Sum256(before), sha256.Sum256(after))
	if *save {
		fmt.Printf("Credential storage: OS credential store, service %s, account %s; an existing saved key is replaced.\n", credentialService, *harness)
	} else {
		fmt.Println("Key is not saved. Future launches need a private environment variable.")
	}
	if *dry {
		fmt.Println("Dry run: no files changed; public discovery does not validate key, balance or compatibility.")
		return nil
	}
	if *smoke {
		answer, err := ask("Allow one small inference request that may be billed? yes/no", "no")
		if err != nil {
			return err
		}
		if answer != "yes" {
			return errors.New("inference check declined; no config changed")
		}
		endpoint := "/v1/responses"
		payload := map[string]any{"model": *model, "input": "Reply with OK.", "store": false, "max_output_tokens": 32}
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
			return errors.New("inference returned an invalid success object")
		}
		if *harness == "claude" && reply["type"] != "message" || *harness != "claude" && reply["status"] != "completed" {
			return errors.New("inference did not complete")
		}
		fmt.Println("Inference completed; verify usage in your dashboard.")
	} else {
		fmt.Println("Key, balance and inference compatibility have not been checked.")
	}
	answer, err := ask("Back up and apply this configuration? yes/no", "no")
	if err != nil {
		return err
	}
	if answer != "yes" {
		return errors.New("cancelled; no config changed")
	}
	backup, err := transactHome(home, target, before, after, existed, func() error {
		written, _, err := readPreview(home, target)
		if err != nil {
			return err
		}
		if !bytes.Equal(written, after) {
			return errors.New("written bytes differ")
		}
		_, err = decodeConfig(*harness, written)
		return err
	})
	if backup != "" {
		fmt.Println("Backup:", backup)
	}
	if err != nil {
		return err
	}
	if *save {
		if err = saveKey(*harness, key); err != nil {
			return err
		}
		fmt.Printf("Launch with: phasesai-setup --launch %s\n", *harness)
	}
	fmt.Println("Config verified. Other auth stores, providers and shell profiles were not removed.")
	if !*save {
		fmt.Println("Export PHASESAI_API_KEY privately in the launch shell; Claude also needs ANTHROPIC_AUTH_TOKEN.")
	}
	return audit.event("apply", "ok", *harness)
}
