package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeCanUseTool(t *testing.T) {
	s := &claudeSession{cwd: "/ws", mcpPrefix: "mcp__tutortop__"}
	for _, c := range []struct {
		name, input string
		ok          bool
	}{
		{"mcp__tutortop__create_task", `{}`, true},
		{"Read", `{"file_path":"/ws/NOTES.md"}`, true},
		{"Read", `{"file_path":"/etc/passwd"}`, false},
		// A field of an unexpected type mustn't hide the path from the check.
		{"Read", `{"file_path":"/etc/passwd","pattern":1}`, false},
		{"Grep", `{"pattern":"x","path":"/elsewhere"}`, false},
		{"Edit", `{"file_path":"/ws/NOTES.md"}`, true},
		{"Edit", `{"file_path":"/ws/tasks/0001/work/a.go"}`, false},
		{"WebSearch", `{"query":"go"}`, true},
		{"Bash", `{"command":"ls"}`, false},
		{"mcp__other__x", `{}`, false},
	} {
		if err := s.canUseTool(c.name, json.RawMessage(c.input)); (err == nil) != c.ok {
			t.Errorf("canUseTool(%s, %s) = %v, want ok=%v", c.name, c.input, err, c.ok)
		}
	}
}

func TestCodexApprovals(t *testing.T) {
	s := &codexSession{cwd: "/ws", mcpName: "tutortop", filePaths: map[string][]string{
		"good": {"/ws/NOTES.md"},
		"bad":  {"/ws/NOTES.md", "/ws/tasks/0001/work/solution.go"},
	}}
	decision := func(method, params string) string {
		t.Helper()
		res, err := s.request(method, json.RawMessage(params))
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		b, _ := json.Marshal(res)
		return string(b)
	}
	cases := []struct{ method, params, want string }{
		{"item/commandExecution/requestApproval", `{"itemId":"x"}`, `"decline"`},
		{"execCommandApproval", `{"command":["ls"]}`, `"denied"`},
		{"item/fileChange/requestApproval", `{"itemId":"good"}`, `"accept"`},
		{"item/fileChange/requestApproval", `{"itemId":"bad"}`, `"decline"`},
		{"item/fileChange/requestApproval", `{"itemId":"unknown"}`, `"decline"`},
		{"applyPatchApproval", `{"fileChanges":{"/ws/NOTES.md":{}}}`, `"approved"`},
		{"applyPatchApproval", `{"fileChanges":{"/ws/tasks/a.go":{}}}`, `"denied"`},
		{"mcpServer/elicitation/request", `{"serverName":"tutortop"}`, `"accept"`},
		{"mcpServer/elicitation/request", `{"serverName":"other"}`, `"decline"`},
	}
	for _, c := range cases {
		if got := decision(c.method, c.params); !strings.Contains(got, c.want) {
			t.Errorf("%s %s = %s, want %s", c.method, c.params, got, c.want)
		}
	}
}
