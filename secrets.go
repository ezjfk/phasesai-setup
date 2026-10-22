package main

import (
	"errors"
	"github.com/zalando/go-keyring"
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
