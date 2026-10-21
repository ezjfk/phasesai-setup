package main

import (
	"context"
	"errors"
	"github.com/zalando/go-keyring"
	"os"
	"strings"
)

const credentialService = "cc.phasesai.setup"

func savedKey(harness string) (string, error) {
	key, err := keyring.Get(credentialService, harness)
	if err != nil {
		return "", errors.New("no accessible saved key; unlock the OS credential store or supply PHASESAI_API_KEY")
	}
	return key, nil
}
func saveKey(harness, key string) error {
	if keyring.Set(credentialService, harness, key) != nil {
		return errors.New("OS credential store rejected the key; configuration is applied, but use a private launch-shell variable")
	}
	return nil
}
func launchHarness(harness string, args []string) error {
	exe, _, err := detectHarness(harness, false)
	if err != nil {
		return err
	}
	key := os.Getenv("PHASESAI_API_KEY")
	if key == "" {
		key, err = savedKey(harness)
		if err != nil {
			return err
		}
	}
	env := []string{}
	for _, value := range os.Environ() {
		name := strings.SplitN(value, "=", 2)[0]
		if strings.EqualFold(name, "PHASESAI_API_KEY") || harness == "claude" && (strings.EqualFold(name, "ANTHROPIC_AUTH_TOKEN") || strings.EqualFold(name, "ANTHROPIC_API_KEY")) {
			continue
		}
		env = append(env, value)
	}
	env = append(env, "PHASESAI_API_KEY="+key)
	if harness == "claude" {
		env = append(env, "ANTHROPIC_AUTH_TOKEN="+key)
	}
	cmd, err := harnessCommand(context.Background(), exe, args...)
	if err != nil {
		return err
	}
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if cmd.Run() != nil {
		return errors.New("harness exited unsuccessfully; inspect its terminal output")
	}
	return nil
}
