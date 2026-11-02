// Package shellenv gives the app the PATH the user has in a terminal.
package shellenv

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// LoadPath adds the PATH of the user's login shell, and the folders tools are
// usually installed in, to this process's PATH. An app opened from Finder or
// a desktop launcher gets a minimal PATH, without the folders where docker,
// podman, claude and codex live, so it wouldn't find them.
func LoadPath() {
	if runtime.GOOS == "windows" {
		return // Windows apps get the user's full PATH
	}
	home, _ := os.UserHomeDir()
	dirs := slices.Concat(
		filepath.SplitList(os.Getenv("PATH")),
		filepath.SplitList(shellPath(loginShell())),
		[]string{
			"/opt/homebrew/bin", "/usr/local/bin", // Homebrew, and Docker Desktop's links
			filepath.Join(home, ".local", "bin"), // Claude Code
			filepath.Join(home, ".docker", "bin"),
		},
	)
	var path []string
	for _, dir := range dirs {
		if dir != "" && !slices.Contains(path, dir) {
			path = append(path, dir)
		}
	}
	os.Setenv("PATH", strings.Join(path, string(os.PathListSeparator)))
}

func loginShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

// shellPath asks an interactive login shell for its PATH, so it includes what
// the user's profile and rc files add. Markers separate it from anything those
// files print.
func shellPath(shell string) string {
	const mark = "__TUTORTOP_PATH__"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-ilc", `printf '`+mark+`%s`+mark+`' "$PATH"`)
	cmd.WaitDelay = time.Second // in case the rc files leave something running
	out, _ := cmd.Output()
	_, path, _ := strings.Cut(string(out), mark)
	path, _, found := strings.Cut(path, mark)
	if !found {
		return ""
	}
	return path
}
