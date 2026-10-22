package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/term"
)

var defaultHarness string // Set by the per-harness EXE build.
var colorOutput bool

func step(n int, label string) {
	if colorOutput {
		fmt.Printf("\n\x1b[36;1m  [%d/5] %s\x1b[0m\n", n, label)
	} else {
		fmt.Printf("\n  [%d/5] %s\n", n, label)
	}
}
func done(label string) {
	if colorOutput {
		fmt.Printf("\x1b[32m      OK  %s\x1b[0m\n", label)
	} else {
		fmt.Println("      OK ", label)
	}
}

func run() error {
	harness := flag.String("harness", defaultHarness, "claude, codex, opencode, or all")
	model := flag.String("model", "", "override the default model")
	config := flag.String("config", "", "override the config path for a single harness (inside your home)")
	dry := flag.Bool("dry-run", false, "preview paths and releases without downloading or writing")
	install := flag.Bool("install-missing", false, "compatibility flag; native installation is now automatic")
	save := flag.Bool("save-key", false, "compatibility flag; credentials are now always persistent")
	recover := flag.Bool("recover", false, "restore an interrupted file transaction; requires --config")
	launch := flag.String("launch", "", "launch an installed harness; arguments follow --")
	logPath := flag.String("log-file", "", "optional private JSONL event log inside your home")
	smoke := flag.Bool("smoke-test", false, "offer a small, potentially billable inference check")
	replace := flag.Bool("replace-key", false, "prompt for a new key instead of reusing configured credentials")
	showVersion := flag.Bool("version", false, "print setup version")
	flag.Parse()
	if *dry && (*install || *save || *logPath != "" || *recover || *launch != "" || *smoke) {
		return errors.New("--dry-run cannot install software, save credentials, create logs, recover, launch or run inference")
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if runtime.GOOS != "windows" {
		return errors.New("PhasesAI setup supports Windows only")
	}
	if *launch != "" {
		return launchHarness(*launch, flag.Args())
	}
	if flag.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if *recover {
		if *config == "" {
			return errors.New("--recover requires the exact --config path")
		}
		path, err := filepath.Abs(*config)
		if err != nil {
			return err
		}
		return recoverTransaction(home, path)
	}
	colorOutput = term.IsTerminal(int(os.Stdout.Fd())) && enableConsoleColor() && os.Getenv("NO_COLOR") == ""
	fmt.Printf("\n  PhasesAI Setup  %s\n  Native Windows setup | %s\n", version, origin)
	if *harness == "" {
		fmt.Println("\n  1  Claude Code\n  2  OpenAI Codex\n  3  OpenCode\n  4  All three")
		value, err := ask("  Choose an app", "4")
		if err != nil {
			return err
		}
		*harness = map[string]string{"1": "claude", "2": "codex", "3": "opencode", "4": "all", "claude": "claude", "codex": "codex", "opencode": "opencode", "all": "all"}[strings.ToLower(value)]
	}
	selected := []string{*harness}
	if *harness == "all" {
		selected = []string{"claude", "codex", "opencode"}
	}
	for _, h := range selected {
		if _, ok := harnessNames[h]; !ok {
			return errors.New("choose claude, codex, opencode, or all")
		}
	}
	if len(selected) > 1 && (*config != "" || *model != "") {
		return errors.New("--config and --model require one harness")
	}
	step(1, "Check your setup")
	paths := map[string]string{}
	plans := map[string]configPlan{}
	models := map[string]string{}
	for _, h := range selected {
		p := defaultConfig(home, h)
		if *config != "" {
			p = *config
		}
		if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
			p = filepath.Join(home, p[2:])
		}
		p, err = filepath.Abs(p)
		if err != nil {
			return err
		}
		plan, err := planFile(home, p, nil)
		if err != nil {
			return err
		}
		if err := checkConfigOverrides(h, plan.Before); err != nil {
			return err
		}
		paths[h], plans[h] = p, plan
		m := "claude-opus-5-5"
		if h == "codex" {
			m = "gpt-6.1-sol"
		}
		if *model != "" {
			m = *model
		}
		if strings.ContainsAny(m, "\r\n\x00") || len(m) > 200 || m == "" {
			return errors.New("invalid model ID")
		}
		if h == "claude" && !strings.HasPrefix(m, "claude-") {
			return errors.New("Claude Code requires a Claude model")
		}
		if h == "opencode" && !strings.HasPrefix(m, "claude-") && !strings.HasPrefix(m, "gpt-") {
			return errors.New("OpenCode setup supports Claude and GPT model IDs")
		}
		models[h] = m
		if _, err := renderPersistent(h, m, "", plan.Before); err != nil {
			return err
		}
		fmt.Printf("      %s: %s\n", harnessNames[h], p)
	}
	done("Existing settings will be merged and backed up")
	step(2, "Connect PhasesAI")
	key := ""
	if !*dry {
		key = os.Getenv("PHASESAI_API_KEY")
		if key == "" && !*replace {
			for _, h := range selected {
				key = configuredKey(home, h, paths[h], plans[h].Before)
				if key != "" {
					break
				}
			}
		}
		if key == "" {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return errors.New("enter your API key in an interactive terminal, or provide PHASESAI_API_KEY privately")
			}
			fmt.Print("      Paste your PhasesAI API key (hidden): ")
			secret, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if err != nil {
				return errors.New("could not read API key")
			}
			key = string(secret)
			clear(secret)
		}
		if err := validateKey(key); err != nil {
			return err
		}
		done("Key will be saved in the app's private configuration")
	} else {
		done("Dry run: no key required")
	}
	step(3, "Find the latest Windows releases")
	releases := map[string]nativeRelease{}
	arch := windowsArchitecture()
	for _, h := range selected {
		r, err := latestRelease(h, arch)
		if err != nil {
			return err
		}
		releases[h] = r
		done(harnessNames[h] + " " + r.Version + " (" + arch + ")")
	}
	if *dry {
		fmt.Println("\n  Preview complete. No downloads, config, credentials, shortcuts or PATH changes.")
		return nil
	}
	var audit *auditLog
	if *logPath != "" {
		audit, err = openAudit(home, *logPath)
		if err != nil {
			return err
		}
		defer audit.close()
		if err := audit.event("start", "ok", *harness); err != nil {
			return err
		}
	}
	step(4, "Download and verify")
	executables := map[string]string{}
	for _, h := range selected {
		fmt.Printf("      Installing %s %s\n", harnessNames[h], releases[h].Version)
		exe, err := installNative(home, h, releases[h])
		if err != nil {
			return err
		}
		executables[h] = exe
		done("SHA-256 and executable version verified")
	}
	step(5, "Save settings and add shortcuts")
	var writes []configPlan
	for _, h := range selected {
		p := plans[h]
		p.After, err = renderPersistent(h, models[h], key, p.Before)
		if err != nil {
			return err
		}
		writes = append(writes, p)
		if h == "codex" {
			auth, err := codexAuth(home, executables[h], key)
			if err != nil {
				return err
			}
			p, err := planFile(home, filepath.Join(filepath.Dir(paths[h]), "auth.json"), auth)
			if err != nil {
				return err
			}
			writes = append(writes, p)
		}
		if *smoke {
			if err := smokeCheck(h, models[h], key); err != nil {
				return err
			}
		}
	}
	if err := applyPlans(home, writes); err != nil {
		return err
	}
	for _, h := range selected {
		if err := persistInstallation(home, h, executables[h], releases[h].Version); err != nil {
			return err
		}
		if err := integrateWindows(home, h, executables[h]); err != nil {
			return fmt.Errorf("settings saved, but Windows shortcuts/PATH need repair: %w; rerun setup", err)
		}
		done(harnessNames[h] + " is ready")
	}
	fmt.Println("\n  Setup complete. Open an app from the PhasesAI Start menu folder,\n  or open a new terminal and run:")
	for _, h := range selected {
		fmt.Println("    " + h)
	}
	fmt.Println("\n  Your API key is saved. Rerun setup anytime to get the latest releases.")
	if !*smoke {
		fmt.Println("  Setup did not send a billable inference request.")
	}
	for _, h := range selected {
		if err := audit.event("apply", "ok", h); err != nil {
			return err
		}
	}
	return nil
}

func validateKey(key string) error {
	if len(key) < 8 || len(key) > 4096 || strings.TrimSpace(key) != key || strings.ContainsAny(key, "\r\n\x00") {
		return errors.New("invalid API key; copy the complete key from your PhasesAI dashboard")
	}
	return nil
}

func configuredKey(home, harness, path string, before []byte) string {
	root, err := decodeConfig(harness, before)
	if err != nil {
		return ""
	}
	var key any
	switch harness {
	case "claude":
		key, _ = fieldValue(root, []string{"env", "ANTHROPIC_AUTH_TOKEN"})
	case "opencode":
		key, _ = fieldValue(root, []string{"provider", "phasesai-anthropic", "options", "apiKey"})
	case "codex":
		data, _, err := readPreview(home, filepath.Join(filepath.Dir(path), "auth.json"))
		if err != nil {
			return ""
		}
		var auth map[string]any
		if parseJSON(data, &auth) != nil {
			return ""
		}
		key = auth["OPENAI_API_KEY"]
	}
	value, _ := key.(string)
	if !strings.HasPrefix(value, "rk_live_") || validateKey(value) != nil {
		return ""
	}
	return value
}

func checkConfigOverrides(harness string, before []byte) error {
	root, err := decodeConfig(harness, before)
	if err != nil {
		return err
	}
	if harness != "claude" {
		conflicts, err := configConflicts(harness, before)
		if err != nil {
			return err
		}
		if len(conflicts) != 0 {
			return fmt.Errorf("custom credential/header settings at %s override saved credentials; remove them before setup", strings.Join(conflicts, ", "))
		}
	}
	if harness == "claude" {
		if helper, ok := root["apiKeyHelper"]; ok && helper != "" {
			return errors.New("Claude apiKeyHelper overrides saved credentials; remove it before setup")
		}
	}
	if harness == "codex" {
		if profile, ok := root["profile"]; ok && profile != "" {
			return errors.New("Codex has an active profile; remove its profile selection before setup")
		}
		if auth, ok := fieldValue(root, []string{"model_providers", "phasesai", "auth"}); ok && auth != nil {
			return errors.New("Codex PhasesAI provider has a credential helper; remove it before setup")
		}
	}
	return nil
}

func smokeCheck(harness, model, key string) error {
	answer, err := ask("      Send one small inference request that may be billed? yes/no", "no")
	if err != nil {
		return err
	}
	if answer != "yes" {
		return errors.New("inference declined; configuration was not changed")
	}
	endpoint := "/v1/responses"
	payload := map[string]any{"model": model, "input": "Reply with OK.", "store": false, "max_output_tokens": 32}
	if harness == "claude" || strings.HasPrefix(model, "claude-") {
		endpoint = "/v1/messages"
		payload = map[string]any{"model": model, "max_tokens": 32, "messages": []any{map[string]any{"role": "user", "content": "Reply with OK."}}}
	}
	body, _ := json.Marshal(payload)
	data, err := request(key, "POST", endpoint, body)
	if err != nil {
		return err
	}
	var reply map[string]any
	if json.Unmarshal(data, &reply) != nil || reply["error"] != nil || (endpoint == "/v1/messages" && reply["type"] != "message") || (endpoint == "/v1/responses" && reply["status"] != "completed") {
		return errors.New("inference did not complete")
	}
	return nil
}
