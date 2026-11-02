//go:build !windows

package shellpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A CLI that only the user's shell config puts on PATH (mise, npm, ~/.local/bin)
// must be found when the app is started from a launcher with a bare PATH, even
// when the config also starts a background process that keeps stdout open.
func TestFindsCLIFromShellConfig(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	home := t.TempDir()
	bin := filepath.Join(home, "tools")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "fake-cli"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := "echo 'shell noise'\nsleep 10 &\nexport PATH=\"" + bin + ":$PATH\"\n"
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", bash)
	t.Setenv("PATH", "/usr/bin:/bin")

	start := time.Now()
	Fix()

	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("Fix waited %s for the background process", took.Round(time.Second))
	}
	if _, err := exec.LookPath("fake-cli"); err != nil {
		t.Errorf("CLI from shell config not found after Fix: PATH=%s", os.Getenv("PATH"))
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Error("lost the original PATH")
	}
}
