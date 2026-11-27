//go:build live

package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestLiveClaudePermissions checks the tutor's file rules against the real
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
	if err := sess.Prompt(ctx, prompt, nil); err != nil {
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

// TestLiveClaudeSubagents checks that a subagent is held to the same rules
// as the tutor (no shell, no edits under tasks/) and that the turn waits for
// it. Uses a little quota.
func TestLiveClaudeSubagents(t *testing.T) {
	ws := t.TempDir()
	for name, content := range map[string]string{"NOTES.md": "x\n", "tasks/a.txt": "x\n"} {
		path := filepath.Join(ws, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var subagents, ended []string
	sess, err := Start(ctx, "claude", Options{
		Cwd: ws, MCPName: "tutortop", MCPURL: "http://127.0.0.1:1/mcp", Subagents: true,
		Instructions: "You are being tested by an automated harness. Follow instructions exactly.",
	}, func(ev Event) {
		switch {
		case ev.Kind == EventTool && ev.Tool.Subagent:
			subagents = append(subagents, ev.Tool.ID)
		case ev.Kind == EventTool && ev.Tool.Status != ToolRunning:
			ended = append(ended, ev.Tool.ID)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	prompt := "Use the Agent tool to start one subagent, and wait for it. Give it exactly this task: " +
		"\"Do all three steps even if one fails: use the Bash tool to run `touch ran.txt`; " +
		"use the Edit tool to change the x to y in tasks/a.txt; use the Edit tool to change the x to y in NOTES.md. Then reply OK.\" " +
		"When it has finished, reply DONE."
	if err := sess.Prompt(ctx, prompt, nil); err != nil {
		t.Fatal(err)
	}

	if len(subagents) == 0 {
		t.Fatal("no subagent ran, so nothing was checked")
	}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(ws, name))
		return strings.TrimSpace(string(b))
	}
	if _, err := os.Stat(filepath.Join(ws, "ran.txt")); err == nil {
		t.Error("the subagent ran a shell command")
	}
	if got := read("tasks/a.txt"); got != "x" {
		t.Errorf("tasks/a.txt = %q, want the subagent's edit refused", got)
	}
	if got := read("NOTES.md"); got != "y" {
		t.Errorf("NOTES.md = %q, want the subagent's edit allowed (did it run its tools?)", got)
	}
	for _, id := range subagents {
		if !slices.Contains(ended, id) {
			t.Errorf("subagent %s hadn't finished when the turn ended", id)
		}
	}
}

// TestLiveAntigravityPermissions checks the profile that holds agy to the
// policy against the real CLI: edits inside the workspace work; edits under
// tasks/ and outside it, commands, subagents and scheduled work are refused.
// A refused permission request ends agy's turn, so each step is its own
// prompt. Uses a little quota.
func TestLiveAntigravityPermissions(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	outside := filepath.Join(root, "outside", "b.txt")
	for path, content := range map[string]string{
		filepath.Join(ws, "NOTES.md"):       "x\n",
		filepath.Join(ws, "tasks", "a.txt"): "x\n",
		outside:                             "x\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	titles := map[string]string{}
	var finished []string // titles of the tool calls that finished
	sess, err := Start(ctx, "antigravity", Options{
		Cwd: ws, MCPName: "tutortop", MCPURL: "http://127.0.0.1:1/mcp", Model: "gemini-3.8-flash",
		Instructions: "You are being tested by an automated harness. Follow instructions exactly.",
	}, func(ev Event) {
		if ev.Kind != EventTool {
			return
		}
		if ev.Tool.Title != "" {
			titles[ev.Tool.ID] = ev.Tool.Title
		}
		if ev.Tool.Status == ToolDone {
			finished = append(finished, titles[ev.Tool.ID])
		}
	})
	if err != nil && strings.Contains(err.Error(), "not signed in") {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	for _, step := range []string{
		"With your file tools, change the x to y in NOTES.md.",
		"With your file tools, change the x to y in tasks/a.txt.",
		"With your file tools, change the x to y in " + outside + ".",
		"Run the shell command `touch ran.txt`.",
		"Use the invoke_subagent tool to start a subagent with the task 'Reply OK.'",
		"Use the schedule tool to schedule a one-time reminder in 5 minutes saying 'hi'.",
	} {
		if err := sess.Prompt(ctx, step+" Don't retry or work around it if it fails. Then reply DONE.", nil); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
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
	if _, err := os.Stat(filepath.Join(ws, "ran.txt")); err == nil {
		t.Error("agy ran a shell command")
	}
	for _, title := range finished {
		if strings.Contains(title, "subagent") || strings.Contains(title, "schedule") {
			t.Errorf("%s ran", title)
		}
	}
	t.Logf("finished tools: %q", finished)
}
