package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeSessionExistsMatchesCLIFolderNames(t *testing.T) {
	config := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	// Underscores, dots and spaces all become "-", like the CLI does.
	cwd := filepath.Join(t.TempDir(), "jane_doe", "Tutortop", "go.basics now")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(cwd)
	name := claudeProjectName(real)
	if strings.ContainsAny(name, "_. /") {
		t.Fatalf("project name %q still has non-alphanumerics", name)
	}
	dir := filepath.Join(config, "projects", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "abc.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !claudeSessionExists(cwd, "abc") {
		t.Fatal("existing session not found")
	}
	if claudeSessionExists(cwd, "missing") {
		t.Fatal("missing session reported as existing")
	}
}

func TestClaudeSessionExistsLongPathsMatchByPrefix(t *testing.T) {
	config := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	cwd := "/" + strings.Repeat("a", 250)
	dir := filepath.Join(config, "projects", claudeProjectName(cwd)[:claudeMaxNameLen]+"-somehash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "abc.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !claudeSessionExists(cwd, "abc") {
		t.Fatal("long-path session not found")
	}
}

func TestCheckWrite(t *testing.T) {
	cwd := "/home/me/Tutortop/go"
	for path, ok := range map[string]bool{
		"NOTES.md":                        true,
		"/home/me/Tutortop/go/MISSION.md": true,
		"learning-records/0001-x.md":      true,
		"tasks/0001-x/work/solution.go":   false,
		"/home/me/Tutortop/go/tasks/a":    false,
		"../other/x":                      false,
		"/etc/passwd":                     false,
	} {
		if err := checkWrite(cwd, path); (err == nil) != ok {
			t.Errorf("checkWrite(%q) = %v, want ok=%v", path, err, ok)
		}
	}
}

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
