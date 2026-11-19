package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// The permission profile and thread settings that hold the tutor to the
// policy and wire up our MCP server.
func TestCodexStart(t *testing.T) {
	opts := codexOptions(t)
	_, cli, _, thread := startFakeCodex(t, opts)

	profile := fmt.Sprintf(`permissions.tutortop.filesystem={":root"="read",%q="write",%q="read"}`, opts.Cwd, filepath.Join(opts.Cwd, "tasks"))
	if want := []string{"app-server", "-c", `default_permissions="tutortop"`, "-c", profile}; !slices.Equal(cli.Args, want) {
		t.Errorf("args %q, want %q", cli.Args, want)
	}
	if got := thread.str("params.approvalPolicy"); got != "untrusted" {
		t.Errorf("thread/start approvalPolicy = %q, want untrusted", got)
	}
	config, _ := thread.get("params.config").(map[string]any)
	if mcp, _ := config["mcp_servers.tutortop"].(map[string]any); mcp["url"] != opts.MCPURL || mcp["tool_timeout_sec"] != 600.0 {
		t.Errorf("config %v doesn't point at our server with a long tool timeout", config)
	}
}

func TestCodexResume(t *testing.T) {
	opts := codexOptions(t)
	opts.ResumeID = "thr_0"
	cli, started, sess, _ := beginCodex(t, opts)
	req := codexExpect(t, cli, "thread/resume")
	if req.str("params.threadId") != "thr_0" || req.str("params.cwd") != opts.Cwd || req.str("params.approvalPolicy") != "untrusted" {
		t.Errorf("thread/resume %v, want the thread and the same settings", req)
	}
	codexReply(t, cli, req, `{"thread":{"id":"thr_0"}}`)
	if err := wait(t, started); err != nil || (*sess).ID() != "thr_0" {
		t.Fatalf("resumed %v, %v; want thr_0", err, *sess)
	}

	// A thread Codex can't resume starts fresh instead of failing.
	cli, started, sess, _ = beginCodex(t, opts)
	codexReplyError(t, cli, codexExpect(t, cli, "thread/resume"), "no rollout found for thread id thr_0")
	codexReply(t, cli, codexExpect(t, cli, "thread/start"), `{"thread":{"id":"thr_2"}}`)
	if err := wait(t, started); err != nil || (*sess).ID() != "thr_2" {
		t.Fatalf("started %v, %v; want a new thread", err, *sess)
	}
}

func TestCodexNotSignedIn(t *testing.T) {
	clis := fakeBinary(t, "codex")
	started := async(func() error {
		_, err := startCodex(t.Context(), codexOptions(t), func(Event) {})
		return err
	})
	cli := clis.next(t)
	codexHandshake(t, cli)
	codexReply(t, cli, codexExpect(t, cli, "account/read"), `{"requiresOpenaiAuth":true,"account":null}`)
	if err := wait(t, started); err == nil || !strings.Contains(err.Error(), "codex login") {
		t.Errorf("Start = %v, want a hint to sign in", err)
	}
}

func TestCodexTurn(t *testing.T) {
	opts := codexOptions(t)
	opts.Effort = "high"
	sess, cli, log, _ := startFakeCodex(t, opts)
	notes := filepath.Join(opts.Cwd, "NOTES.md")

	done := prompt(t, sess, "hello")
	start := codexExpect(t, cli, "turn/start")
	if start.str("params.threadId") != "thr_1" || start.str("params.input.0.text") != "hello" || start.str("params.effort") != "high" {
		t.Errorf("turn/start %v, want the thread, text and effort", start)
	}
	codexReply(t, cli, start, `{"turn":{"id":"turn_1","status":"inProgress","items":[]}}`)
	cli.send(t,
		`{"method":"turn/started","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"inProgress","items":[]}}}`,
		`{"method":"item/started","params":{"threadId":"thr_1","turnId":"turn_1","item":{"type":"reasoning","id":"rs_1","summary":[],"content":[]}}}`,
		`{"method":"item/started","params":{"threadId":"thr_1","turnId":"turn_1","item":{"type":"agentMessage","id":"msg_1","text":""}}}`,
		`{"method":"item/agentMessage/delta","params":{"threadId":"thr_1","turnId":"turn_1","itemId":"msg_1","delta":"Let me "}}`,
		`{"method":"item/agentMessage/delta","params":{"threadId":"thr_1","turnId":"turn_1","itemId":"msg_1","delta":"look."}}`,
		`{"method":"item/completed","params":{"threadId":"thr_1","turnId":"turn_1","item":{"type":"agentMessage","id":"msg_1","text":"Let me look."}}}`,
		`{"method":"item/started","params":{"threadId":"thr_1","turnId":"turn_1","item":{"type":"commandExecution","id":"cmd_1","command":"go test\n./...","cwd":"/ws","status":"inProgress"}}}`,
		`{"method":"item/completed","params":{"threadId":"thr_1","turnId":"turn_1","item":{"type":"commandExecution","id":"cmd_1","command":"go test\n./...","cwd":"/ws","status":"declined"}}}`,
		`{"method":"item/started","params":{"threadId":"thr_1","turnId":"turn_1","item":{"type":"fileChange","id":"fc_1","changes":[{"path":`+jsonString(notes)+`,"kind":{"type":"update"},"diff":""}],"status":"inProgress"}}}`,
		`{"method":"item/completed","params":{"threadId":"thr_1","turnId":"turn_1","item":{"type":"fileChange","id":"fc_1","changes":[{"path":`+jsonString(notes)+`,"kind":{"type":"update"},"diff":""}],"status":"completed"}}}`,
		`{"method":"item/started","params":{"threadId":"thr_1","turnId":"turn_1","item":{"type":"mcpToolCall","id":"mcp_1","server":"tutortop","tool":"create_task","status":"inProgress","arguments":{}}}}`,
		`{"method":"item/completed","params":{"threadId":"thr_1","turnId":"turn_1","item":{"type":"mcpToolCall","id":"mcp_1","server":"tutortop","tool":"create_task","status":"failed","arguments":{}}}}`,
		`{"method":"item/started","params":{"threadId":"thr_1","turnId":"turn_1","item":{"type":"webSearch","id":"ws_1","query":"goroutines"}}}`,
		`{"method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed","items":[]}}}`,
	)
	if err := wait(t, done); err != nil {
		t.Fatal(err)
	}
	run := Tool{ID: "cmd_1", Kind: ToolExecute}
	edit := Tool{ID: "fc_1", Kind: ToolEdit, Paths: []string{notes}}
	mcp := Tool{ID: "mcp_1", Kind: ToolOther, MCPServer: "tutortop", MCPTool: "create_task"}
	want := []Event{
		{Kind: EventText, Text: "Let me "},
		{Kind: EventText, Text: "look."},
		{Kind: EventTool, Tool: withStatus(run, ToolRunning)},
		{Kind: EventTool, Tool: withStatus(run, ToolFailed)},
		{Kind: EventTool, Tool: withStatus(edit, ToolRunning)},
		{Kind: EventTool, Tool: withStatus(edit, ToolDone)},
		{Kind: EventTool, Tool: withStatus(mcp, ToolRunning)},
		{Kind: EventTool, Tool: withStatus(mcp, ToolFailed)},
		{Kind: EventTool, Tool: Tool{ID: "ws_1", Status: ToolRunning, Kind: ToolFetch}},
	}
	if got := untitled(log.take()); !reflect.DeepEqual(got, want) {
		t.Errorf("events:\n%+v\nwant:\n%+v", got, want)
	}

	// A failed turn is an error.
	done = prompt(t, sess, "again")
	codexReply(t, cli, codexExpect(t, cli, "turn/start"), `{"turn":{"id":"turn_2","status":"inProgress","items":[]}}`)
	cli.send(t, `{"method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_2","status":"failed","items":[],"error":{"message":"usage limit reached"}}}}`)
	if err := wait(t, done); err == nil || !strings.Contains(err.Error(), "usage limit reached") || errors.Is(err, ErrExited) {
		t.Errorf("failed turn: %v, want Codex's reason", err)
	}
}

func TestCodexCancel(t *testing.T) {
	sess, cli, _, _ := startFakeCodex(t, codexOptions(t))
	done := prompt(t, sess, "write an essay")
	codexReply(t, cli, codexExpect(t, cli, "turn/start"), `{"turn":{"id":"turn_1","status":"inProgress","items":[]}}`)
	waitTurn(t, sess.(*codexSession))
	sess.Cancel()
	req := codexExpect(t, cli, "turn/interrupt")
	if req.str("params.threadId") != "thr_1" || req.str("params.turnId") != "turn_1" {
		t.Errorf("turn/interrupt %v, want this thread's turn", req)
	}
	codexReply(t, cli, req, `{}`)
	cli.send(t, `{"method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"interrupted","items":[]}}}`)
	if err := wait(t, done); err != nil {
		t.Errorf("interrupted turn: %v, want nil", err)
	}

	// A Cancel before Codex names the turn interrupts it once it does.
	done = prompt(t, sess, "write another")
	start := codexExpect(t, cli, "turn/start")
	sess.Cancel()
	codexReply(t, cli, start, `{"turn":{"id":"turn_2","status":"inProgress","items":[]}}`)
	req = codexExpect(t, cli, "turn/interrupt")
	if req.str("params.turnId") != "turn_2" {
		t.Errorf("turn/interrupt %v, want the turn that started after Cancel", req)
	}
	codexReply(t, cli, req, `{}`)
	cli.send(t, `{"method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_2","status":"interrupted","items":[]}}}`)
	if err := wait(t, done); err != nil {
		t.Errorf("turn cancelled before it started: %v, want nil", err)
	}
}

func TestCodexExit(t *testing.T) {
	sess, cli, _, _ := startFakeCodex(t, codexOptions(t))
	done := prompt(t, sess, "hi")
	codexReply(t, cli, codexExpect(t, cli, "turn/start"), `{"turn":{"id":"turn_1","status":"inProgress","items":[]}}`)
	cli.exit(t, 1, "thread 'main' panicked")
	if err := wait(t, done); !errors.Is(err, ErrExited) || !strings.Contains(err.Error(), "panicked") {
		t.Errorf("Prompt = %v, want ErrExited with the CLI's stderr", err)
	}
	if err := wait(t, prompt(t, sess, "again")); !errors.Is(err, ErrExited) {
		t.Errorf("Prompt after exit = %v, want ErrExited", err)
	}
}

func TestCodexPermissions(t *testing.T) {
	opts := codexOptions(t)
	_, cli, _, _ := startFakeCodex(t, opts)
	n := 100
	// ask sends a request from the app-server and returns our result.
	ask := func(t *testing.T, method string, params any) message {
		t.Helper()
		n++
		cli.send(t, map[string]any{"id": n, "method": method, "params": params})
		reply := cli.recv(t)
		if reply.get("id") != float64(n) || reply.get("method") != nil {
			t.Fatalf("reply %v doesn't answer request %d", reply, n)
		}
		return reply
	}
	turn := map[string]any{"threadId": "thr_1", "turnId": "turn_1", "startedAtMs": 0}
	with := func(extra map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range turn {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	testPolicy(t, opts.Cwd, func(t *testing.T, tool Tool) bool {
		switch {
		case tool.MCPServer != "":
			r := ask(t, "mcpServer/elicitation/request", with(map[string]any{"serverName": tool.MCPServer, "mode": "form", "message": "Pick one", "requestedSchema": map[string]any{}}))
			return r.str("result.action") == "accept"
		case tool.Kind == ToolExecute:
			r := ask(t, "item/commandExecution/requestApproval", with(map[string]any{"itemId": "cmd_1", "command": "ls"}))
			return r.str("result.decision") == "accept"
		case tool.Kind == ToolEdit:
			// Approvals name only the item; its paths came with item/started.
			id := fmt.Sprint("fc_", n)
			cli.send(t, map[string]any{"method": "item/started", "params": with(map[string]any{"item": map[string]any{
				"type": "fileChange", "id": id, "status": "inProgress",
				"changes": []any{map[string]any{"path": tool.Paths[0], "kind": map[string]any{"type": "update"}, "diff": ""}},
			}})})
			r := ask(t, "item/fileChange/requestApproval", with(map[string]any{"itemId": id}))
			return r.str("result.decision") == "accept"
		case tool.Kind == ToolRead:
			t.Skip("Codex doesn't ask before reading: its permission profile lets it read anywhere")
		case tool.Subagent:
			t.Skip("Codex has no subagent tool")
		}
		t.Fatalf("no Codex request for %+v", tool)
		return false
	})

	tasks := filepath.Join(opts.Cwd, "tasks", "0001", "work", "main.go")
	for _, c := range []struct {
		name, method string
		params       map[string]any
		want         string // in the result
	}{
		{
			name:   "file change Codex never described",
			method: "item/fileChange/requestApproval",
			params: with(map[string]any{"itemId": "fc_unknown"}),
			want:   `"decline"`,
		},
		// What older app-servers send.
		{
			name:   "v1 patch inside",
			method: "applyPatchApproval",
			params: map[string]any{"conversationId": "thr_1", "callId": "c", "fileChanges": map[string]any{filepath.Join(opts.Cwd, "NOTES.md"): map[string]any{}}},
			want:   `"approved"`,
		},
		{
			name:   "v1 patch touching tasks",
			method: "applyPatchApproval",
			params: map[string]any{"conversationId": "thr_1", "callId": "c", "fileChanges": map[string]any{filepath.Join(opts.Cwd, "NOTES.md"): map[string]any{}, tasks: map[string]any{}}},
			want:   `"denied"`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, _ := json.Marshal(ask(t, c.method, c.params)["result"])
			if !strings.Contains(string(b), c.want) {
				t.Errorf("result %s, want %s", b, c.want)
			}
		})
	}
	t.Run("other requests", func(t *testing.T) {
		// e.g. asking for more filesystem or network access.
		r := ask(t, "item/permissions/requestApproval", with(map[string]any{"itemId": "p", "permissions": map[string]any{}}))
		if r.get("error") == nil || r.get("result") != nil {
			t.Errorf("reply %v, want an error", r)
		}
	})
}

func codexOptions(t *testing.T) Options {
	return Options{Cwd: t.TempDir(), MCPName: "tutortop", MCPURL: "http://127.0.0.1:1/mcp", Instructions: "Be a tutor."}
}

// startFakeCodex starts the Codex adapter on a fake app-server, through a
// new thread, and returns the thread/start request too.
func startFakeCodex(t *testing.T, opts Options) (Session, *fakeCLI, *eventLog, message) {
	t.Helper()
	cli, started, sess, log := beginCodex(t, opts)
	thread := codexExpect(t, cli, "thread/start")
	codexReply(t, cli, thread, `{"thread":{"id":"thr_1"}}`)
	if err := wait(t, started); err != nil {
		t.Fatal(err)
	}
	return *sess, cli, log, thread
}

// beginCodex starts the Codex adapter on a fake app-server, up to the point
// where it opens a thread. *sess is set once started is done.
func beginCodex(t *testing.T, opts Options) (cli *fakeCLI, started <-chan error, sess *Session, log *eventLog) {
	t.Helper()
	clis := fakeBinary(t, "codex")
	var s Session
	log, sess = &eventLog{}, &s
	started = async(func() (err error) {
		*sess, err = startCodex(t.Context(), opts, log.add)
		if err == nil {
			t.Cleanup(func() { _ = (*sess).Close() })
		}
		return err
	})
	cli = clis.next(t)
	codexHandshake(t, cli)
	codexReply(t, cli, codexExpect(t, cli, "account/read"), `{"requiresOpenaiAuth":true,"account":{"type":"chatgpt","email":"a@b.c","planType":"plus"}}`)
	return cli, started, sess, log
}

// codexHandshake plays the app-server's side of initialize.
func codexHandshake(t *testing.T, cli *fakeCLI) {
	t.Helper()
	req := codexExpect(t, cli, "initialize")
	codexReply(t, cli, req, `{"userAgent":"codex/0.166.0"}`)
	if n := cli.recv(t); n.str("method") != "initialized" || n.get("id") != nil {
		t.Errorf("sent %v, want the initialized notification", n)
	}
}

// codexExpect returns the adapter's next message, which must be a request
// for method.
func codexExpect(t *testing.T, cli *fakeCLI, method string) message {
	t.Helper()
	m := cli.recv(t)
	if m.str("method") != method || m.get("id") == nil {
		t.Fatalf("sent %v, want a %s request", m, method)
	}
	return m
}

func codexReply(t *testing.T, cli *fakeCLI, req message, result string) {
	t.Helper()
	id, _ := json.Marshal(req.get("id"))
	cli.send(t, fmt.Sprintf(`{"id":%s,"result":%s}`, id, strings.Join(strings.Fields(result), " ")))
}

func codexReplyError(t *testing.T, cli *fakeCLI, req message, msg string) {
	t.Helper()
	cli.send(t, map[string]any{"id": req.get("id"), "error": map[string]any{"code": -32600, "message": msg}})
}

// waitTurn waits until Prompt has the turn's id, which Cancel needs.
func waitTurn(t *testing.T, s *codexSession) {
	t.Helper()
	for deadline := time.Now().Add(fakeTimeout); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		s.mu.Lock()
		id := s.turnID
		s.mu.Unlock()
		if id != "" {
			return
		}
	}
	t.Fatal("the turn never started")
}

func withStatus(t Tool, s ToolStatus) Tool {
	t.Status = s
	return t
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
