package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// The tutor may read and change files inside the learner's workspace, except
// under tasks/, which the app manages through create_task, and the settings
// for git and the agent CLIs (configPaths). It may use
// Tutortop's MCP tools and the web, and, where Options.Subagents allows,
// subagents that finish within the turn. It may not run commands, use other
// MCP servers, or start background work that outlives a turn.
//
// An adapter enforces this in two layers:
//
//  1. It starts the CLI so the CLI refuses what it can by itself: no shell,
//     no writes under tasks/, configPaths or outside the workspace, only our
//     MCP server.
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
	subagents bool   // Options.Subagents
}

func newPolicy(opts Options) policy {
	return policy{cwd: opts.Cwd, mcpServer: opts.MCPName, subagents: opts.Subagents}
}

// allow decides one tool call, or explains why it's refused. The reason goes
// back to the agent, so it says what to do instead.
func (p policy) allow(t Tool) error {
	if t.Subagent {
		if !p.subagents {
			return errNoSubagents
		}
		return nil
	}
	if t.MCPServer != "" {
		if t.MCPServer != p.mcpServer {
			return fmt.Errorf("only the %s tools are available, not %s", p.mcpServer, t.MCPServer)
		}
		return nil
	}
	switch t.Kind {
	case ToolExecute:
		return errNoShell
	case ToolOther:
		return errUnavailable
	case ToolEdit:
		// A change whose paths we never saw is declined rather than guessed at.
		if len(t.Paths) == 0 {
			return errors.New("unknown file change")
		}
		for _, path := range t.Paths {
			if err := p.checkWrite(path); err != nil {
				return err
			}
		}
		return nil
	case ToolRead, ToolSearch, ToolFetch, ToolThink:
		for _, path := range t.Paths {
			if _, inside := p.workspaceRel(path); !inside {
				return errors.New("only files inside the learner's workspace are available")
			}
		}
		return nil
	}
	return errUnavailable // a kind this policy doesn't know yet
}

var (
	errNoShell     = errors.New("running commands isn't available in Tutortop. Use the tutortop tools: create_task runs code in the sandbox")
	errNoSubagents = errors.New("subagents aren't available in this conversation. Do the work yourself, in this turn")
	errUnavailable = errors.New("not available in Tutortop. Use the tutortop tools: create_task runs code in the sandbox")
)

// checkWrite explains why the tutor may not change path, or returns nil.
func (p policy) checkWrite(path string) error {
	rel, inside := p.workspaceRel(path)
	if !inside {
		return fmt.Errorf("only files inside the learner's workspace can be changed (not %s)", path)
	}
	first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	if first == "tasks" {
		return fmt.Errorf("tasks are managed by the app; use create_task instead of editing %s", rel)
	}
	// Folding case: on macOS and Windows .Claude is .claude.
	if i := slices.IndexFunc(configPaths, func(c string) bool { return strings.EqualFold(c, first) }); i >= 0 {
		return fmt.Errorf("%s holds settings for git or the agent, which can't be changed from a conversation", configPaths[i])
	}
	return nil
}

// configPaths are the workspace's settings for git and the agent CLIs. A
// change there could get round the policy: a git hook or an agent's hook runs
// a command, and an agent's project settings can add MCP servers or allow
// tools.
var configPaths = []string{".git", ".agents", ".claude", ".codex", ".gemini", ".mcp.json"}

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
