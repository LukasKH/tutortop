package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The flags that hold the tutor to the policy and wire up our MCP server.
func TestClaudeStart(t *testing.T) {
	for _, subagents := range []bool{false, true} {
		t.Run(fmt.Sprint("subagents ", subagents), func(t *testing.T) {
			opts := claudeOptions(t)
			opts.Subagents = subagents
			sess, cli, _ := startFakeClaude(t, opts)

			for flag, want := range map[string]string{
				"--permission-mode":        "acceptEdits",
				"--permission-prompt-tool": "stdio",
				"--allowedTools":           "mcp__tutortop__*",
				"--session-id":             sess.ID(),
			} {
				if got := cli.arg(flag); got != want {
					t.Errorf("%s %q, want %q", flag, got, want)
				}
			}
			// The shell and tasks/ stay shut; Agent and Task only where subagents are off.
			for tool, shut := range map[string]bool{"Bash": true, "Edit(./tasks/**)": true, "Agent": !subagents, "Task": !subagents} {
				if slices.Contains(cli.Args, tool) != shut {
					t.Errorf("%s disallowed = %v, want %v: %q", tool, !shut, shut, cli.Args)
				}
			}
			if !slices.Contains(cli.Args, "--strict-mcp-config") {
				t.Error("other MCP servers aren't shut out")
			}
			var mcp struct {
				MCPServers map[string]struct{ Type, URL string } `json:"mcpServers"`
			}
			if err := json.Unmarshal([]byte(cli.arg("--mcp-config")), &mcp); err != nil || mcp.MCPServers["tutortop"].URL != opts.MCPURL {
				t.Errorf("--mcp-config %s doesn't point at our server (%v)", cli.arg("--mcp-config"), err)
			}
			if !slices.Contains(cli.Env, "MCP_TOOL_TIMEOUT=600000") {
				t.Error("MCP tool calls get Claude's default timeout, too short for create_task")
			}
			if !slices.Contains(cli.Env, "CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1") {
				t.Error("background tasks are on, so work can outlive the turn")
			}
		})
	}
}

func TestClaudeResume(t *testing.T) {
	opts := claudeOptions(t)
	const saved = "4b4c1f2e-0000-4000-8000-000000000001"
	cwd, _ := filepath.EvalSymlinks(opts.Cwd)
	// Claude Code names a project's folder after its path, with every
	// character but a letter or digit replaced by '-'.
	project := regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(cwd, "-")
	transcript := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", project, saved+".jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	opts.ResumeID = saved
	sess, cli, _ := startFakeClaude(t, opts)
	if cli.arg("--resume") != saved || sess.ID() != saved {
		t.Errorf("resume %q, id %q; want both %q", cli.arg("--resume"), sess.ID(), saved)
	}

	// A session Claude Code no longer has starts fresh instead of failing.
	opts.ResumeID = "4b4c1f2e-0000-4000-8000-000000000002"
	sess, cli, _ = startFakeClaude(t, opts)
	if cli.arg("--resume") != "" || sess.ID() == opts.ResumeID || cli.arg("--session-id") != sess.ID() {
		t.Errorf("args %q, id %q; want a new session", cli.Args, sess.ID())
	}
}

func TestClaudeTurn(t *testing.T) {
	opts := claudeOptions(t)
	sess, cli, log := startFakeClaude(t, opts)
	notes := filepath.Join(opts.Cwd, "NOTES.md")

	done := prompt(t, sess, "hello")
	msg := cli.recv(t)
	if msg.str("type") != "user" || msg.str("message.content") != "hello" {
		t.Fatalf("sent %v, want the user message", msg)
	}
	cli.send(t,
		`{"type":"system","subtype":"init","mcp_servers":[{"name":"tutortop","status":"connected"}]}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Let me "}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"look."}}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Let me look."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"`+notes+`"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"x"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"mcp__tutortop__create_task","input":{}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"gate failed"}]}}`,
		// A subagent's own text isn't taken for the tutor's reply.
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a1","name":"Agent","input":{"description":"Find sources","prompt":"..."}}]}}`,
		`{"type":"stream_event","parent_tool_use_id":"a1","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Report for the tutor"}}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a1","content":"found 3"}]}}`,
		`{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Done."}}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"Let me look."}`,
	)
	if err := wait(t, done); err != nil {
		t.Fatal(err)
	}
	want := []Event{
		{Kind: EventText, Text: "Let me "},
		{Kind: EventText, Text: "look."},
		{Kind: EventTool, Tool: Tool{ID: "t1", Status: ToolRunning, Kind: ToolRead, Paths: []string{notes}}},
		{Kind: EventTool, Tool: Tool{ID: "t1", Status: ToolDone}},
		{Kind: EventTool, Tool: Tool{ID: "t2", Status: ToolRunning, Kind: ToolOther, MCPServer: "tutortop", MCPTool: "create_task"}},
		{Kind: EventTool, Tool: Tool{ID: "t2", Status: ToolFailed}},
		{Kind: EventTool, Tool: Tool{ID: "a1", Status: ToolRunning, Kind: ToolOther, Subagent: true}},
		{Kind: EventTool, Tool: Tool{ID: "a1", Status: ToolDone}},
		{Kind: EventText, Text: "Done."},
	}
	if got := untitled(log.take()); !reflect.DeepEqual(got, want) {
		t.Errorf("events:\n%+v\nwant:\n%+v", got, want)
	}

	// A failed turn is an error.
	done = prompt(t, sess, "again")
	cli.recv(t)
	cli.send(t, `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Credit balance is too low"}`)
	if err := wait(t, done); err == nil || !strings.Contains(err.Error(), "Credit balance is too low") || errors.Is(err, ErrExited) {
		t.Errorf("failed turn: %v, want Claude's reason", err)
	}
}

// Without our MCP server the tutor can't do its job, so the learner hears of
// it, once.
func TestClaudeMCPWarning(t *testing.T) {
	for _, c := range []struct {
		name     string
		statuses []string // in successive init messages
		warn     bool
	}{
		{"pending, then connected", []string{"pending", "connected"}, false},
		{"failed twice", []string{"failed", "failed"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			sess, cli, log := startFakeClaude(t, claudeOptions(t))
			for _, status := range c.statuses {
				done := prompt(t, sess, "hi")
				cli.recv(t)
				servers := fmt.Sprintf(`[{"name":"other","status":"failed"},{"name":"tutortop","status":%q}]`, status)
				cli.send(t,
					`{"type":"system","subtype":"init","mcp_servers":`+servers+`}`,
					`{"type":"result","subtype":"success","is_error":false}`)
				if err := wait(t, done); err != nil {
					t.Fatal(err)
				}
			}
			var warnings int
			for _, ev := range log.take() {
				if ev.Kind == EventWarning {
					warnings++
				}
			}
			if want := map[bool]int{false: 0, true: 1}[c.warn]; warnings != want {
				t.Errorf("%d warnings, want %d", warnings, want)
			}
		})
	}
}

// Claude Code ends an interrupted turn with an error result; that's a
// normal stop, but only for the turn that was interrupted.
func TestClaudeCancel(t *testing.T) {
	sess, cli, _ := startFakeClaude(t, claudeOptions(t))
	done := prompt(t, sess, "write an essay")
	cli.recv(t)
	sess.Cancel()
	req := cli.recv(t)
	if req.str("type") != "control_request" || req.str("request.subtype") != "interrupt" {
		t.Fatalf("Cancel sent %v, want an interrupt", req)
	}
	cli.send(t, claudeControlOK(req),
		`{"type":"result","subtype":"error_during_execution","is_error":true}`)
	if err := wait(t, done); err != nil {
		t.Errorf("interrupted turn: %v, want nil", err)
	}

	done = prompt(t, sess, "next")
	cli.recv(t)
	cli.send(t, `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"overloaded"}`)
	if err := wait(t, done); err == nil {
		t.Error("a failed turn after an interrupted one wasn't reported")
	}
}

func TestClaudeExit(t *testing.T) {
	t.Run("during a turn", func(t *testing.T) {
		sess, cli, _ := startFakeClaude(t, claudeOptions(t))
		done := prompt(t, sess, "hi")
		cli.recv(t)
		cli.exit(t, 1, "Error: session expired, run claude login")
		err := wait(t, done)
		if !errors.Is(err, ErrExited) || !strings.Contains(err.Error(), "run claude login") {
			t.Errorf("Prompt = %v, want ErrExited with the CLI's stderr", err)
		}
		if err := wait(t, prompt(t, sess, "again")); !errors.Is(err, ErrExited) {
			t.Errorf("Prompt after exit = %v, want ErrExited", err)
		}
	})
	t.Run("while starting", func(t *testing.T) {
		clis := fakeBinary(t, "claude")
		started := async(func() error {
			_, err := startClaude(t.Context(), claudeOptions(t), func(Event) {})
			return err
		})
		cli := clis.next(t)
		cli.recv(t)
		cli.exit(t, 1, "unknown option --effort")
		if err := wait(t, started); !errors.Is(err, ErrExited) || !strings.Contains(err.Error(), "unknown option --effort") {
			t.Errorf("Start = %v, want ErrExited with the CLI's stderr", err)
		}
	})
}

func TestClaudePermissions(t *testing.T) {
	opts := claudeOptions(t)
	_, cli, _ := startFakeClaude(t, opts)
	n := 0
	// ask sends a can_use_tool request and returns the answer.
	ask := func(t *testing.T, name string, input any) message {
		t.Helper()
		n++
		cli.send(t, map[string]any{"type": "control_request", "request_id": fmt.Sprint("cli_", n),
			"request": map[string]any{"subtype": "can_use_tool", "tool_name": name, "input": input}})
		reply := cli.recv(t)
		if reply.str("type") != "control_response" || reply.str("response.request_id") != fmt.Sprint("cli_", n) {
			t.Fatalf("reply %v doesn't answer the request", reply)
		}
		return reply
	}
	allowed := func(t *testing.T, name string, input any) bool {
		t.Helper()
		reply := ask(t, name, input)
		switch reply.str("response.response.behavior") {
		case "allow":
			// Claude Code runs the tool with updatedInput, so it must be the
			// input that was checked.
			if !reflect.DeepEqual(reply.get("response.response.updatedInput"), input) {
				t.Errorf("updatedInput %v, want the input %v", reply.get("response.response.updatedInput"), input)
			}
			return true
		case "deny":
			if reply.str("response.response.message") == "" {
				t.Error("denied without telling the agent why")
			}
			return false
		}
		t.Fatalf("reply %v neither allows nor denies", reply)
		return false
	}

	testPolicy(t, opts.Cwd, func(t *testing.T, tool Tool) bool {
		switch {
		case tool.MCPServer != "":
			return allowed(t, "mcp__"+tool.MCPServer+"__"+tool.MCPTool, map[string]any{})
		case tool.Kind == ToolExecute:
			return allowed(t, "Bash", map[string]any{"command": "ls"})
		case tool.Kind == ToolEdit:
			return allowed(t, "Edit", map[string]any{"file_path": tool.Paths[0], "old_string": "x", "new_string": "y"})
		case tool.Kind == ToolRead:
			return allowed(t, "Read", map[string]any{"file_path": tool.Paths[0]})
		case tool.Subagent:
			return allowed(t, "Agent", map[string]any{"description": "Find sources", "prompt": "Find the Go spec on errors."})
		}
		t.Fatalf("no Claude tool for %+v", tool)
		return false
	})

	outside := filepath.Join(filepath.Dir(opts.Cwd), "elsewhere")
	for _, c := range []struct {
		name  string
		tool  string
		input any
		allow bool
	}{
		{"search outside", "Grep", map[string]any{"pattern": "x", "path": outside}, false},
		{"web search", "WebSearch", map[string]any{"query": "goroutines"}, true},
		// A field of an unexpected type mustn't hide the path from the check.
		{"odd field beside an outside path", "Read", map[string]any{"file_path": outside, "pattern": 1.0}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := allowed(t, c.tool, c.input); got != c.allow {
				t.Errorf("allowed = %v, want %v", got, c.allow)
			}
		})
	}

	t.Run("other requests", func(t *testing.T) {
		cli.send(t, `{"type":"control_request","request_id":"cli_x","request":{"subtype":"hook_callback","callback_id":"1"}}`)
		if reply := cli.recv(t); reply.str("response.subtype") != "error" {
			t.Errorf("reply %v, want an error", reply)
		}
	})
}

// claudeOptions are the options the Claude tests start with. Claude Code's
// own config folder is a temporary one, so resuming never sees the real one.
func claudeOptions(t *testing.T) Options {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	return Options{Cwd: t.TempDir(), MCPName: "tutortop", MCPURL: "http://127.0.0.1:1/mcp", Instructions: "Be a tutor."}
}

// startFakeClaude starts the Claude adapter on a fake CLI, through the
// initialize handshake.
func startFakeClaude(t *testing.T, opts Options) (Session, *fakeCLI, *eventLog) {
	t.Helper()
	clis := fakeBinary(t, "claude")
	log := &eventLog{}
	var sess Session
	started := async(func() (err error) {
		sess, err = startClaude(t.Context(), opts, log.add)
		return err
	})
	cli := clis.next(t)
	req := cli.recv(t)
	if req.str("type") != "control_request" || req.str("request.subtype") != "initialize" {
		t.Fatalf("first message %v, want initialize", req)
	}
	cli.send(t, claudeControlOK(req))
	if err := wait(t, started); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess, cli, log
}

// claudeControlOK answers one of the adapter's control requests.
func claudeControlOK(req message) string {
	return fmt.Sprintf(`{"type":"control_response","response":{"subtype":"success","request_id":%q}}`, req.str("request_id"))
}

// prompt runs sess.Prompt while the test plays the CLI.
func prompt(t *testing.T, sess Session, text string) <-chan error {
	return async(func() error { return sess.Prompt(t.Context(), text) })
}
