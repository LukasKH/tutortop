package agent

import (
	"path/filepath"
	"testing"
)

// testPolicy checks that an adapter puts the CLI's permission requests
// through the policy. Every adapter runs it with an ask that makes its fake
// CLI ask about tool and reports whether the adapter allowed it. For a kind
// of call its CLI never asks about, ask calls t.Skip and says why.
func testPolicy(t *testing.T, cwd string, ask func(t *testing.T, tool Tool) (allowed bool)) {
	in := func(rel string) string { return filepath.Join(cwd, rel) }
	outside := filepath.Join(filepath.Dir(cwd), "outside.txt")
	for _, c := range []struct {
		name  string
		tool  Tool
		allow bool
	}{
		{"run a command", Tool{Kind: ToolExecute, Title: "Ran ls"}, false},
		{"edit in the workspace", Tool{Kind: ToolEdit, Paths: []string{in("NOTES.md")}}, true},
		{"edit under tasks", Tool{Kind: ToolEdit, Paths: []string{in("tasks/0001-x/work/main.go")}}, false},
		{"edit outside the workspace", Tool{Kind: ToolEdit, Paths: []string{outside}}, false},
		{"edit escaping with ..", Tool{Kind: ToolEdit, Paths: []string{cwd + string(filepath.Separator) + filepath.Join("..", "outside.txt")}}, false},
		{"read in the workspace", Tool{Kind: ToolRead, Paths: []string{in("MISSION.md")}}, true},
		{"read outside the workspace", Tool{Kind: ToolRead, Paths: []string{outside}}, false},
		{"use our MCP server", Tool{Kind: ToolOther, MCPServer: "tutortop", MCPTool: "get_state"}, true},
		{"use another MCP server", Tool{Kind: ToolOther, MCPServer: "other", MCPTool: "x"}, false},
		{"start a subagent where they're off", Tool{Kind: ToolOther, Subagent: true}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ask(t, c.tool); got != c.allow {
				t.Errorf("allowed = %v, want %v", got, c.allow)
			}
		})
	}
}

// The rules the adapters' CLIs don't exercise: relative paths, several paths,
// and calls the policy can't place.
func TestPolicy(t *testing.T) {
	p := policy{cwd: "/ws", mcpServer: "tutortop"}
	for _, c := range []struct {
		name string
		tool Tool
		ok   bool
	}{
		{"relative edit under tasks", Tool{Kind: ToolEdit, Paths: []string{"./tasks/a.go"}}, false},
		{"edit the tasks folder", Tool{Kind: ToolEdit, Paths: []string{"/ws/tasks"}}, false},
		{"edit a file named like tasks", Tool{Kind: ToolEdit, Paths: []string{"/ws/tasks.md"}}, true},
		{"one of several paths under tasks", Tool{Kind: ToolEdit, Paths: []string{"/ws/NOTES.md", "/ws/tasks/a.go"}}, false},
		{"add a git hook", Tool{Kind: ToolEdit, Paths: []string{"/ws/.git/hooks/pre-commit"}}, false},
		{"change Claude Code's project settings", Tool{Kind: ToolEdit, Paths: []string{"/ws/.claude/settings.json"}}, false},
		{"change them in another case", Tool{Kind: ToolEdit, Paths: []string{"/ws/.Claude/settings.json"}}, false},
		{"add an Antigravity MCP server", Tool{Kind: ToolEdit, Paths: []string{"./.agents/mcp_config.json"}}, false},
		{"add a project MCP server", Tool{Kind: ToolEdit, Paths: []string{"/ws/.mcp.json"}}, false},
		{"change Codex's project settings", Tool{Kind: ToolEdit, Paths: []string{"/ws/.codex/config.toml"}}, false},
		{"edit a folder named like a setting deeper down", Tool{Kind: ToolEdit, Paths: []string{"/ws/notes/.claude/x.md"}}, true},
		{"edit .gitignore", Tool{Kind: ToolEdit, Paths: []string{"/ws/.gitignore"}}, true},
		{"relative read escaping", Tool{Kind: ToolSearch, Paths: []string{"../x"}}, false},
		{"unknown tool", Tool{Kind: ToolOther, Title: "NotebookEdit"}, false},
		{"MCP tool with a path outside", Tool{Kind: ToolOther, MCPServer: "tutortop", Paths: []string{"/etc"}}, true},
	} {
		if err := p.allow(c.tool); (err == nil) != c.ok {
			t.Errorf("%s: allow = %v, want ok=%v", c.name, err, c.ok)
		}
	}
	p.subagents = true
	if err := p.allow(Tool{Kind: ToolOther, Subagent: true}); err != nil {
		t.Errorf("subagent refused where they're on: %v", err)
	}
}
