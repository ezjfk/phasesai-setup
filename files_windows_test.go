//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsNewPrivateFileHasCurrentUserOwner(t *testing.T) {
	f, err := os.OpenFile(filepath.Join(t.TempDir(), "new-private-file"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = secureFile(f); err != nil {
		t.Fatal(err)
	}
	if _, err = inspectFile(f); err != nil {
		t.Fatalf("new private file must have user ownership even with an elevated default owner: %v", err)
	}
}
