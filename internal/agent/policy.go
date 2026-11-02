package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// The tutor may read files inside the learner's workspace and change them,
// except under tasks/, which the app manages. It may not run commands.
// Claude and Codex enforce this themselves where they can (see claude.go and
// codex.go); allowTool decides what they still ask us about.

var errNoShell = errors.New("running commands isn't available in Tutortop. Use the tutortop tools: create_task runs code in the sandbox")

// allowTool decides a tool call an agent asks permission for, or explains
// why it's refused.
func allowTool(cwd string, kind ToolKind, paths []string) error {
	switch kind {
	case ToolExecute:
		return errNoShell
	case ToolEdit:
		// A change whose paths we never saw is declined rather than guessed at.
		if len(paths) == 0 {
			return errors.New("unknown file change")
		}
		for _, p := range paths {
			if err := checkWrite(cwd, p); err != nil {
				return err
			}
		}
	default:
		for _, p := range paths {
			if _, inside := workspaceRel(cwd, p); !inside {
				return errors.New("only files inside the learner's workspace are available")
			}
		}
	}
	return nil
}

// workspaceRel returns path relative to the workspace, and whether it stays
// inside the workspace.
func workspaceRel(cwd, path string) (string, bool) {
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return "", false
		}
		path = rel
	}
	path = filepath.Clean(path)
	return path, path == "." || filepath.IsLocal(path)
}

// checkWrite explains why the tutor may not change path, or returns nil.
func checkWrite(cwd, path string) error {
	rel, inside := workspaceRel(cwd, path)
	if !inside {
		return fmt.Errorf("only files inside the learner's workspace can be changed (not %s)", path)
	}
	if rel == "tasks" || strings.HasPrefix(rel, "tasks"+string(filepath.Separator)) {
		return fmt.Errorf("tasks are managed by the app; use create_task instead of editing %s", rel)
	}
	return nil
}
