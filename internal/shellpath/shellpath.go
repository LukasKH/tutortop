//go:build !windows

// Package shellpath gives the app the same PATH as the user's terminal.
//
// Apps started from a desktop launcher (always on macOS, often on Linux) don't
// run the user's shell config, so CLIs installed with mise, npm, Homebrew or
// into ~/.local/bin look missing. Windows apps get the user's PATH from the
// registry, so Fix does nothing there.
package shellpath

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const marker = "__TUTORTOP_PATH__"

// Fix sets this process's PATH to the login shell's PATH, followed by any
// other folders from the current PATH and common install folders.
func Fix() {
	home, _ := os.UserHomeDir()
	_ = os.Setenv("PATH", merge(loginShellPath(os.Getenv("SHELL")), filepath.SplitList(os.Getenv("PATH")), commonDirs(home)))
}

// loginShellPath asks an interactive login shell for its PATH. Shell config
// can print anything, so the value is read from between two markers.
func loginShellPath(shell string) []string {
	if shell == "" {
		shell = "/bin/sh"
	}
	script := `printf '%s%s%s' "` + marker + `" "$PATH" "` + marker + `"`
	if filepath.Base(shell) == "fish" {
		script = `printf '%s%s%s' '` + marker + `' (string join : $PATH) '` + marker + `'`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", script)
	// Its own session, so the interactive shell can't take over a terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// Without these, oh-my-zsh can stop to ask about updates and tmux
	// plugins can start a session (from sindresorhus/shell-env).
	cmd.Env = append(os.Environ(), "DISABLE_AUTO_UPDATE=true", "ZSH_TMUX_AUTOSTART=false", "ZSH_TMUX_AUTOSTARTED=true")
	// A process started by the shell config can keep stdout open after the
	// shell exits; stop waiting for it a second later.
	cmd.WaitDelay = time.Second
	// The error is ignored: the shell can print the PATH and still fail, for
	// example when that leftover process trips WaitDelay. parse needs both
	// markers, so partial output is rejected.
	out, _ := cmd.Output()
	return parse(string(out))
}

func parse(out string) []string {
	_, rest, ok := strings.Cut(out, marker)
	if !ok {
		return nil
	}
	value, _, ok := strings.Cut(rest, marker)
	if !ok {
		return nil
	}
	return filepath.SplitList(value)
}

// commonDirs are where the Claude Code, Codex and Antigravity installers, npm
// and Docker Desktop put binaries, in case the shell lookup failed.
func commonDirs(home string) []string {
	dirs := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".docker", "bin"),
		)
	}
	return dirs
}

// merge joins the lists into one PATH, keeping the first copy of each folder.
func merge(lists ...[]string) string {
	var dirs []string
	for _, d := range slices.Concat(lists...) {
		if d != "" && !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}
