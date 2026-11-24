//go:build !windows

// Package subproc configures background child processes.
package subproc

import "os/exec"

// HideWindow keeps cmd from opening a visible console window on Windows.
// Outside Windows it does nothing.
func HideWindow(*exec.Cmd) {}
