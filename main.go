// Open source PhasesAI setup. No telemetry or self-update.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
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

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Setup:", err)
		os.Exit(1)
	}
}
