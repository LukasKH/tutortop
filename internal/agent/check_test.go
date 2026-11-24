package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Claude Code's check reads its own words, so these play it with shell
// scripts that answer --version and `auth status`. Codex and Antigravity
// answer from a session: see TestCodexCheck and TestAntigravityModels.
func TestCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLIs are shell scripts")
	}
	claude, _ := Lookup(string(Claude))
	for _, c := range []struct {
		name     string
		provider Provider
		script   string
		version  string
		account  string
		problem  string // a part of it; "" for none
	}{
		{
			name:     "Claude Code signed in with a plan",
			provider: claude,
			script:   `case "$1" in --version) echo "2.1.331 (Claude Code)";; auth) echo '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"max"}';; esac`,
			version:  "2.1.331",
			account:  "Claude Max",
		},
		{
			name:     "Claude Code warning on stderr",
			provider: claude,
			script:   `case "$1" in --version) echo "2.1.331 (Claude Code)";; auth) echo "warning: config is old" >&2; echo '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"pro"}';; esac`,
			version:  "2.1.331",
			account:  "Claude Pro",
		},
		{
			name:     "Claude Code signed out",
			provider: claude,
			script:   `case "$1" in --version) echo "2.1.331 (Claude Code)";; auth) echo '{"loggedIn":false}'; exit 1;; esac`,
			version:  "2.1.331",
			problem:  "not signed in to Claude Code",
		},
		{
			name:     "a CLI that won't run",
			provider: claude,
			script:   `echo "broken install" >&2; exit 3`,
			problem:  "broken install",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "cli")
			if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+c.script+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			got := c.provider.Check(t.Context(), bin)
			if got.Version != c.version || got.Account != c.account {
				t.Errorf("version %q, account %q; want %q, %q", got.Version, got.Account, c.version, c.account)
			}
			if c.problem == "" && got.Problem != "" || !strings.Contains(got.Problem, c.problem) {
				t.Errorf("problem %q, want one with %q", got.Problem, c.problem)
			}
			if c.problem == "" && len(got.Models) == 0 {
				t.Error("no models listed")
			}
		})
	}
}
