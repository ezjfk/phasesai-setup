//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestUserPathUpgradePreservesUnrelatedEntries(t *testing.T) {
	root := `C:\Users\Test\AppData\Local\PhasesAI\harnesses\codex`
	current := `C:\Other;` + root + `\old\bin;C:\Windows\System32;C:\Existing Node`
	next := root + `\new\bin`
	out := updateUserPath(current, next, root)
	if strings.Contains(out, `\old\`) || !strings.HasPrefix(out, next+";") || !strings.Contains(out, `C:\Existing Node`) || updateUserPath(out, next, root) != out {
		t.Fatal("PATH update lost entries, duplicated, or retained old managed installation")
	}
}

func TestShortcutPathsArePowerShellLiterals(t *testing.T) {
	value := `C:\Users\O'Brien\$(do-not-run)\tool.exe`
	if powershellLiteral(value) != `'C:\Users\O''Brien\$(do-not-run)\tool.exe'` {
		t.Fatal("unsafe shortcut path quoting")
	}
}
