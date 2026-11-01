//go:build live

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveClaudePermissions checks the coach's file rules against the real
// CLI: edits inside the workspace work, edits under tasks/ and outside the
// workspace are refused. The MCP URL points nowhere, so the "tools didn't
// connect" warning must fire too. Uses a little quota.
func TestLiveClaudePermissions(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	for path, content := range map[string]string{
		filepath.Join(ws, "NOTES.md"):           "x\n",
		filepath.Join(ws, "tasks", "a.txt"):     "x\n",
		filepath.Join(root, "outside", "b.txt"): "x\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var warnings []string
	sess, err := Start(ctx, "claude", Options{
		Cwd: ws, MCPName: "tutortop", MCPURL: "http://127.0.0.1:1/mcp",
		Instructions: "You are being tested by an automated harness. Follow instructions exactly.",
	}, func(ev Event) {
		if ev.Kind == EventWarning {
			warnings = append(warnings, ev.Text)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	outside := filepath.Join(root, "outside", "b.txt")
	prompt := "Use the Edit tool three times, even if one fails: change the x to y in tasks/a.txt, in NOTES.md, and in " + outside + ". Then reply DONE."
	if err := sess.Prompt(ctx, prompt); err != nil {
		t.Fatal(err)
	}

	read := func(p string) string { b, _ := os.ReadFile(p); return strings.TrimSpace(string(b)) }
	if got := read(filepath.Join(ws, "NOTES.md")); got != "y" {
		t.Errorf("NOTES.md = %q, want the edit to be allowed", got)
	}
	if got := read(filepath.Join(ws, "tasks", "a.txt")); got != "x" {
		t.Errorf("tasks/a.txt = %q, want the edit to be refused", got)
	}
	if got := read(outside); got != "x" {
		t.Errorf("outside file = %q, want the edit to be refused", got)
	}
	if len(warnings) == 0 {
		t.Error("no warning about the unreachable MCP server")
	}
	t.Logf("warnings: %v", warnings)
}
