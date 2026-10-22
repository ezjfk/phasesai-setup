// Open source Windows setup. No telemetry; harness releases resolve at runtime.
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
	pause := len(os.Args) == 1 && ownConsole()
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "\n  Setup stopped:", err)
	}
	if pause {
		fmt.Print("\n  Press Enter to close...")
		_, _ = input.ReadString('\n')
	}
	if err != nil {
		os.Exit(1)
	}
}
