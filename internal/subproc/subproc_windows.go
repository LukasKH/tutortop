//go:build windows

// Package subproc configures background child processes.
package subproc

import (
	"os/exec"
	"syscall"
)

// createNoWindow is the Win32 CREATE_NO_WINDOW process creation flag.
// On Windows 11 (where Windows Terminal is the default console host),
// HideWindow alone can still flash a terminal window; setting both stops
// Windows from allocating a console for background child processes.
const createNoWindow = 0x08000000

// HideWindow keeps cmd from opening a visible console window on Windows.
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
