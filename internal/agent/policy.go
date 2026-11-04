package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// The tutor may read and change files inside the learner's workspace, except
// under tasks/, which the app manages through create_task. It may use
// Tutortop's MCP tools and the web. It may not run commands, use other MCP
// servers, or start background work that outlives a turn.
//
// An adapter enforces this in two layers:
//
//  1. It starts the CLI so the CLI refuses what it can by itself: no shell,
//     no writes under tasks/ or outside the workspace, only our MCP server.
//     Never a mode that approves everything.
//  2. It answers every permission request the CLI still makes with
//     [policy.allow], describing the call as a [Tool]. A request it can't
//     describe, or doesn't know, is refused.
//
// testPolicy in policy_test.go checks layer 2 for each adapter through the
// fake CLI.

// policy decides the tool calls a session's CLI asks about.
type policy struct {
	cwd       string // the workspace
	mcpServer string // our MCP server's name
}

func newPolicy(opts Options) policy { return policy{cwd: opts.Cwd, mcpServer: opts.MCPName} }

// allow decides one tool call, or explains why it's refused. The reason goes
// back to the agent, so it says what to do instead.
func (p policy) allow(t Tool) error {
	switch {
	case t.MCPServer != "":
		if t.MCPServer != p.mcpServer {
			return fmt.Errorf("only the %s tools are available, not %s", p.mcpServer, t.MCPServer)
		}
		return nil
	case t.Kind == ToolExecute:
		return errNoShell
	case t.Kind == ToolOther:
		return errUnavailable
	case t.Kind == ToolEdit:
		// A change whose paths we never saw is declined rather than guessed at.
		if len(t.Paths) == 0 {
			return errors.New("unknown file change")
		}
		for _, path := range t.Paths {
			if err := p.checkWrite(path); err != nil {
				return err
			}
		}
	default:
		for _, path := range t.Paths {
			if _, inside := p.workspaceRel(path); !inside {
				return errors.New("only files inside the learner's workspace are available")
			}
		}
	}
	return nil
}

var (
	errNoShell     = errors.New("running commands isn't available in Tutortop. Use the tutortop tools: create_task runs code in the sandbox")
	errUnavailable = errors.New("not available in Tutortop. Use the tutortop tools: create_task runs code in the sandbox")
)

// checkWrite explains why the tutor may not change path, or returns nil.
func (p policy) checkWrite(path string) error {
	rel, inside := p.workspaceRel(path)
	if !inside {
		return fmt.Errorf("only files inside the learner's workspace can be changed (not %s)", path)
	}
	if rel == "tasks" || strings.HasPrefix(rel, "tasks"+string(filepath.Separator)) {
		return fmt.Errorf("tasks are managed by the app; use create_task instead of editing %s", rel)
	}
	return nil
}

// workspaceRel returns path relative to the workspace, and whether it stays
// inside the workspace. A relative path is taken as relative to it.
func (p policy) workspaceRel(path string) (string, bool) {
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(p.cwd, path)
		if err != nil {
			return "", false
		}
		path = rel
	}
	path = filepath.Clean(path)
	return path, path == "." || filepath.IsLocal(path)
}
