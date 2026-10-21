package main

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// Run alone in a separate process so the mock unit-test backend is never used.
// The native workflow creates disposable unlocked stores on macOS and Linux.
func TestNativeKeyringRoundTrip(t *testing.T) {
	if os.Getenv("PHASESAI_NATIVE_KEYRING_TEST") != "1" {
		t.Skip("explicit native-store integration test")
	}
	account, value := "acceptance-"+nonce(), nonce()+nonce()
	defer keyring.Delete(credentialService, account)
	if err := saveKey(account, value); err != nil {
		t.Fatal("native credential store write failed")
	}
	got, err := savedKey(account)
	if err != nil || got != value {
		t.Fatal("native credential store read failed or mismatched")
	}
	if err := keyring.Delete(credentialService, account); err != nil {
		t.Fatal("native credential store cleanup failed")
	}
	if _, err := keyring.Get(credentialService, account); err == nil {
		t.Fatal("deleted native credential remains readable")
	}
}
