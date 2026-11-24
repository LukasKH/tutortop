package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/adrg/xdg"
)

// How agy is started and configured: the profile, the settings that hold it
// to the policy, the session's MCP server, instructions, model and effort.
func TestAntigravityStart(t *testing.T) {
	opts := antigravityOptions(t)
	settings := filepath.Join(antigravityProfile(), "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	// What `agy acp --login` leaves behind.
	if err := os.WriteFile(settings, []byte(`{"auth":{"type":"oauth-personal"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	opts.Model, opts.Effort = "gemini-3.8-flash", "low"
	cli, started, _, _ := beginAntigravity(t, opts)

	if want := []string{"acp", "--gemini-dir", antigravityProfile()}; !slices.Equal(cli.Args, want) {
		t.Errorf("args %q, want %q", cli.Args, want)
	}
	if cli.Dir != opts.Cwd {
		t.Errorf("ran in %s, want the workspace", cli.Dir)
	}
	got, _ := os.ReadFile(settings)
	if want := map[string]any{"auth": map[string]any{"type": "oauth-personal"}, "toolPermission": "strict"}; !reflect.DeepEqual(decodeMap(t, got), want) {
		t.Errorf("settings.json = %s, want strict and the sign-in method kept", got)
	}
	hooks, _ := os.ReadFile(filepath.Join(antigravityProfile(), "config", "hooks.json"))
	for _, tool := range []string{"run_command", "invoke_subagent", "schedule"} {
		if !strings.Contains(string(hooks), tool) || !strings.Contains(string(hooks), `\"decision\":\"deny\"`) {
			t.Errorf("hooks.json = %s, doesn't deny %s", hooks, tool)
		}
	}

	req := acpExpect(t, cli, "session/new")
	if req.str("params.cwd") != opts.Cwd || req.str("params._meta.antigravity.systemPrompt.append") != opts.Instructions {
		t.Errorf("session/new %v, want the workspace and the instructions appended", req)
	}
	if want := []any{map[string]any{"type": "http", "name": "tutortop", "url": opts.MCPURL, "headers": []any{}}}; !reflect.DeepEqual(req.get("params.mcpServers"), want) {
		t.Errorf("mcpServers %v, want only ours", req.get("params.mcpServers"))
	}
	acpReply(t, cli, req, `{"sessionId":"s1","configOptions":`+antigravityConfigOptions+`}`)
	for _, want := range []struct{ id, value string }{{"model", "gemini-3.8-flash"}, {"thought_level", "low"}} {
		req := acpExpect(t, cli, "session/set_config_option")
		if req.str("params.sessionId") != "s1" || req.str("params.configId") != want.id || req.str("params.value") != want.value {
			t.Errorf("set_config_option %v, want %s=%s", req, want.id, want.value)
		}
		acpReply(t, cli, req, `{"configOptions":[]}`)
	}
	if err := wait(t, started); err != nil {
		t.Fatal(err)
	}
}

func TestAntigravityResume(t *testing.T) {
	opts := antigravityOptions(t)
	opts.ResumeID = "s0"
	cli, started, sess, _ := beginAntigravity(t, opts)
	req := acpExpect(t, cli, "session/resume")
	if req.str("params.sessionId") != "s0" || req.str("params.cwd") != opts.Cwd ||
		req.str("params._meta.antigravity.systemPrompt.append") != opts.Instructions || req.str("params.mcpServers.0.url") != opts.MCPURL {
		t.Errorf("session/resume %v, want the session and the same settings", req)
	}
	acpReply(t, cli, req, `{}`)
	if err := wait(t, started); err != nil || (*sess).ID() != "s0" {
		t.Fatalf("resumed %v, %v; want s0", err, *sess)
	}

	// A session agy can't resume starts fresh instead of failing.
	cli, started, sess, _ = beginAntigravity(t, opts)
	acpReplyError(t, cli, acpExpect(t, cli, "session/resume"), -32002, "Resource not found: s0")
	acpReply(t, cli, acpExpect(t, cli, "session/new"), `{"sessionId":"s2"}`)
	if err := wait(t, started); err != nil || (*sess).ID() != "s2" {
		t.Fatalf("started %v, %v; want a new session", err, *sess)
	}
}

func TestAntigravityNotSignedIn(t *testing.T) {
	cli, started, _, _ := beginAntigravity(t, antigravityOptions(t))
	acpReplyError(t, cli, acpExpect(t, cli, "session/new"), -32000, "Authentication required")
	want := `agy acp --login --gemini-dir "` + antigravityProfile() + `"`
	if err := wait(t, started); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Start = %v, want a hint to run %s", err, want)
	}
}

func TestAntigravityTurn(t *testing.T) {
	opts := antigravityOptions(t)
	sess, cli, log := startFakeAntigravity(t, opts)
	notes := filepath.Join(opts.Cwd, "NOTES.md")

	done := prompt(t, sess, "hello")
	req := acpExpect(t, cli, "session/prompt")
	if req.str("params.sessionId") != "s1" || req.str("params.prompt.0.text") != "hello" {
		t.Errorf("session/prompt %v, want the session and text", req)
	}
	cli.send(t,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"Thinking about it."}}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Let me "}}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"look."}}}}`,
		// agy reports in_progress, pending while it asks, in_progress again.
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"tool_call","toolCallId":"call_1","name":"mcp_tutortop_get_state","args":{"toolSummary":"Get state"},"status":"in_progress","kind":"other","title":"Run mcp_tutortop_get_state","rawInput":{"toolSummary":"Get state"}}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_1","name":"mcp_tutortop_get_state","status":"pending","kind":"other","title":"Run mcp_tutortop_get_state"}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_1","name":"mcp_tutortop_get_state","status":"in_progress","kind":"other","title":"Run mcp_tutortop_get_state"}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_1","name":"mcp_tutortop_get_state","status":"completed","kind":"other","title":"Run mcp_tutortop_get_state"}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"tool_call","toolCallId":"call_2","name":"write_file","args":{"Content":"hi\n","Path":`+jsonString(notes)+`},"status":"in_progress","kind":"edit","title":"File NOTES.md","locations":[{"path":`+jsonString(notes)+`}]}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_2","name":"write_file","status":"completed","kind":"edit","title":"File NOTES.md","content":[{"type":"diff","path":`+jsonString(notes)+`,"newText":"hi\n"}]}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"tool_call","toolCallId":"call_3","name":"run_command","args":{"CommandLine":"ls"},"status":"in_progress","kind":"execute","title":"ls","rawInput":{"CommandLine":"ls"}}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_3","name":"run_command","status":"failed","kind":"execute","title":"ls","rawOutput":"tool call denied by pre-tool hook: not available in Tutortop"}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"usage_update","used":11126,"size":1048576}}}`,
	)
	acpReply(t, cli, req, `{"stopReason":"end_turn","usage":{"inputTokens":12768,"outputTokens":577,"totalTokens":21746}}`)
	if err := wait(t, done); err != nil {
		t.Fatal(err)
	}
	mcp := Tool{ID: "call_1", Kind: ToolOther, MCPServer: "tutortop", MCPTool: "get_state"}
	edit := Tool{ID: "call_2", Kind: ToolEdit, Paths: []string{notes}}
	run := Tool{ID: "call_3", Kind: ToolExecute}
	want := []Event{
		{Kind: EventText, Text: "Let me "},
		{Kind: EventText, Text: "look."},
		{Kind: EventTool, Tool: withStatus(mcp, ToolRunning)},
		{Kind: EventTool, Tool: Tool{ID: "call_1", Status: ToolDone}},
		{Kind: EventTool, Tool: withStatus(edit, ToolRunning)},
		{Kind: EventTool, Tool: Tool{ID: "call_2", Status: ToolDone}},
		{Kind: EventTool, Tool: withStatus(run, ToolRunning)},
		{Kind: EventTool, Tool: Tool{ID: "call_3", Status: ToolFailed}},
	}
	if got := untitled(log.take()); !reflect.DeepEqual(got, want) {
		t.Errorf("events:\n%+v\nwant:\n%+v", got, want)
	}

	// A failed turn is an error, and the session goes on.
	done = prompt(t, sess, "again")
	acpReplyError(t, cli, acpExpect(t, cli, "session/prompt"), -32603, "quota exceeded for model gemini-4-neon")
	if err := wait(t, done); err == nil || !strings.Contains(err.Error(), "quota exceeded") || errors.Is(err, ErrExited) {
		t.Errorf("failed turn: %v, want agy's reason", err)
	}
	done = prompt(t, sess, "once more")
	acpReply(t, cli, acpExpect(t, cli, "session/prompt"), `{"stopReason":"refusal"}`)
	if err := wait(t, done); err == nil || !strings.Contains(err.Error(), "refusal") {
		t.Errorf("refused turn: %v, want an error", err)
	}
	done = prompt(t, sess, "and again")
	acpReply(t, cli, acpExpect(t, cli, "session/prompt"), `{"stopReason":"end_turn"}`)
	if err := wait(t, done); err != nil {
		t.Errorf("turn after a failed one: %v", err)
	}
}

func TestAntigravityCancel(t *testing.T) {
	sess, cli, _ := startFakeAntigravity(t, antigravityOptions(t))
	done := prompt(t, sess, "write an essay")
	req := acpExpect(t, cli, "session/prompt")
	sess.Cancel()
	if n := cli.recv(t); n.str("method") != "session/cancel" || n.get("id") != nil || n.str("params.sessionId") != "s1" {
		t.Errorf("sent %v, want the session/cancel notification", n)
	}
	// A permission request still open when the turn was cancelled.
	cli.send(t, `{"jsonrpc":"2.0","id":"p1","method":"session/request_permission","params":{"sessionId":"s1","toolCall":{"toolCallId":"call_1","name":"read_file","kind":"read","title":"File a.txt","locations":[{"path":"a.txt"}]},"options":[{"optionId":"allow","name":"Allow","kind":"allow_once"},{"optionId":"deny","name":"Deny","kind":"reject_once"}]}}`)
	if r := cli.recv(t); r.str("result.outcome.outcome") != "cancelled" {
		t.Errorf("permission reply %v, want the cancelled outcome", r)
	}
	acpReply(t, cli, req, `{"stopReason":"cancelled"}`)
	if err := wait(t, done); err != nil {
		t.Errorf("cancelled turn: %v, want nil", err)
	}

	// Without a turn, Cancel sends nothing; the next turn isn't cancelled.
	sess.Cancel()
	done = prompt(t, sess, "next")
	req = acpExpect(t, cli, "session/prompt")
	cli.send(t, `{"jsonrpc":"2.0","id":"p2","method":"session/request_permission","params":{"sessionId":"s1","toolCall":{"toolCallId":"call_2","name":"read_file","kind":"read","title":"File a.txt","locations":[{"path":"a.txt"}]},"options":[{"optionId":"allow","name":"Allow","kind":"allow_once"},{"optionId":"deny","name":"Deny","kind":"reject_once"}]}}`)
	if r := cli.recv(t); r.str("result.outcome.optionId") != "allow" {
		t.Errorf("permission reply %v in the next turn, want allow", r)
	}
	acpReply(t, cli, req, `{"stopReason":"end_turn"}`)
	if err := wait(t, done); err != nil {
		t.Errorf("turn after a cancelled one: %v", err)
	}
}

func TestAntigravityPromptContext(t *testing.T) {
	sess, cli, _ := startFakeAntigravity(t, antigravityOptions(t))
	ctx, cancel := context.WithCancel(t.Context())
	done := async(func() error { return sess.Prompt(ctx, "hi") })
	acpExpect(t, cli, "session/prompt")
	cancel()
	if err := wait(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("Prompt = %v, want ctx's error", err)
	}
}

// A settings file that holds null, as a hand edit can leave it.
func TestAntigravityNullSettings(t *testing.T) {
	opts := antigravityOptions(t)
	settings := filepath.Join(antigravityProfile(), "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	startFakeAntigravity(t, opts)
	if got, _ := os.ReadFile(settings); decodeMap(t, got)["toolPermission"] != "strict" {
		t.Errorf("settings.json = %s, want strict", got)
	}
}

func TestAntigravityExit(t *testing.T) {
	sess, cli, _ := startFakeAntigravity(t, antigravityOptions(t))
	done := prompt(t, sess, "hi")
	acpExpect(t, cli, "session/prompt")
	cli.exit(t, 1, "panic: runtime error")
	if err := wait(t, done); !errors.Is(err, ErrExited) || !strings.Contains(err.Error(), "panic") {
		t.Errorf("Prompt = %v, want ErrExited with the CLI's stderr", err)
	}
	if err := wait(t, prompt(t, sess, "again")); !errors.Is(err, ErrExited) {
		t.Errorf("Prompt after exit = %v, want ErrExited", err)
	}

	// While starting.
	cli, started, _, _ := beginAntigravity(t, antigravityOptions(t))
	acpExpect(t, cli, "session/new")
	cli.exit(t, 2, "unknown flag: --gemini-dir")
	if err := wait(t, started); !errors.Is(err, ErrExited) || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("Start = %v, want ErrExited with the CLI's stderr", err)
	}
}

func TestAntigravityModels(t *testing.T) {
	antigravityOptions(t) // for the profile
	clis := fakeBinary(t, "agy")
	var models []Model
	done := async(func() (err error) {
		models, err = antigravityModels(t.Context())
		return err
	})
	cli := clis.next(t)
	acpReply(t, cli, acpExpect(t, cli, "initialize"), antigravityInitialize)
	acpReply(t, cli, acpExpect(t, cli, "session/new"), `{"sessionId":"s1","configOptions":`+antigravityConfigOptions+`}`)
	if err := wait(t, done); err != nil {
		t.Fatal(err)
	}
	efforts := []Effort{{ID: "low", Description: "Low"}, {ID: "high", Description: "High"}}
	want := []Model{
		{ID: "gemini-4-neon", Name: "Gemini 4 Neon", Efforts: efforts},
		{ID: "gemini-3.8-flash", Name: "Gemini 3.8 Flash", Efforts: efforts},
		{ID: "claude-sonnet-5-5", Name: "Claude Sonnet 5.5", Efforts: efforts},
	}
	if !reflect.DeepEqual(models, want) {
		t.Errorf("models:\n%+v\nwant:\n%+v", models, want)
	}
}

func TestAntigravityPermissions(t *testing.T) {
	opts := antigravityOptions(t)
	_, cli, _ := startFakeAntigravity(t, opts)
	n := 100
	// ask sends a request from agy and returns our reply.
	ask := func(t *testing.T, method string, params any) message {
		t.Helper()
		n++
		cli.send(t, map[string]any{"jsonrpc": "2.0", "id": n, "method": method, "params": params})
		reply := cli.recv(t)
		if reply.get("id") != float64(n) || reply.get("method") != nil {
			t.Fatalf("reply %v doesn't answer request %d", reply, n)
		}
		return reply
	}

	testPolicy(t, opts.Cwd, func(t *testing.T, tool Tool) bool {
		call := map[string]any{"toolCallId": fmt.Sprint("call_", n), "status": "pending", "kind": string(tool.Kind)}
		switch {
		case tool.MCPServer != "":
			call["name"] = "mcp_" + tool.MCPServer + "_" + tool.MCPTool
		case tool.Subagent:
			call["name"] = "invoke_subagent"
		case tool.Kind == ToolExecute:
			call["name"] = "run_command"
		case tool.Kind == ToolEdit:
			call["name"] = "write_file"
		case tool.Kind == ToolRead:
			call["name"] = "read_file"
		}
		var locations []any
		for _, p := range tool.Paths {
			locations = append(locations, map[string]any{"path": p})
		}
		if locations != nil {
			call["locations"] = locations
		}
		r := ask(t, "session/request_permission", map[string]any{"sessionId": "s1", "toolCall": call, "options": []any{
			map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
			map[string]any{"optionId": "deny", "name": "Deny", "kind": "reject_once"},
		}})
		return r.str("result.outcome.optionId") == "allow"
	})

	// Calls the shared cases don't cover, which agy could describe this way.
	for _, c := range []struct {
		name string
		call map[string]any
	}{
		{"another server's tool, said to read", map[string]any{"toolCallId": "c1", "name": "mcp_other_fetch", "kind": "read"}},
		{"our server, no tool", map[string]any{"toolCallId": "c2", "name": "mcp_tutortop_", "kind": "other"}},
		{"edit at an empty path", map[string]any{"toolCallId": "c3", "name": "write_file", "kind": "edit", "locations": []any{map[string]any{"path": ""}}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ask(t, "session/request_permission", map[string]any{"sessionId": "s1", "toolCall": c.call, "options": []any{
				map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
				map[string]any{"optionId": "deny", "name": "Deny", "kind": "reject_once"},
			}})
			if r.str("result.outcome.optionId") != "deny" {
				t.Errorf("reply %v, want deny", r)
			}
		})
	}

	// agy may call the client's file and terminal methods only if we offer
	// them, and we don't.
	for _, method := range []string{"fs/write_text_file", "fs/read_text_file", "terminal/create"} {
		t.Run(method, func(t *testing.T) {
			r := ask(t, method, map[string]any{"sessionId": "s1", "path": filepath.Join(opts.Cwd, "a.txt"), "content": "x", "command": "ls"})
			if r.get("error") == nil || r.get("result") != nil {
				t.Errorf("reply %v, want an error", r)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(opts.Cwd, "a.txt")); err == nil {
		t.Error("fs/write_text_file wrote the file")
	}
}

// antigravityInitialize is agy's initialize reply, shortened.
const antigravityInitialize = `{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"promptCapabilities":{"image":true,"audio":false,"embeddedContext":true},"sessionCapabilities":{"resume":{},"close":{},"load":{},"list":{}},"_meta":{"antigravity":{"tier":true,"systemPrompt":{"append":true}}}},"agentInfo":{"name":"Antigravity ACP Server","version":"1.5.16"},"authMethods":[{"id":"oauth-personal","name":"Log in with Google"}]}`

// antigravityConfigOptions are a session's config options, shortened.
const antigravityConfigOptions = `[` +
	`{"id":"model","name":"Model","category":"model","type":"select","currentValue":"gemini-4-neon","options":[{"value":"gemini-3.8-flash","name":"Gemini 3.8 Flash"},{"value":"gemini-4-neon","name":"Gemini 4 Neon"},{"value":"claude-sonnet-5-5","name":"Claude Sonnet 5.5"}]},` +
	`{"id":"thought_level","name":"Thinking Level","category":"thought_level","type":"select","currentValue":"high","options":[{"value":"low","name":"Low"},{"value":"high","name":"High"}]},` +
	`{"id":"mode","name":"Mode","category":"mode","type":"select","currentValue":"default","options":[{"value":"default","name":"Default"},{"value":"accept-edits","name":"Accept edits"}]}]`

// antigravityOptions are a session's options, with Tutortop's data folder,
// and so agy's profile, in a temporary folder.
func antigravityOptions(t *testing.T) Options {
	t.Cleanup(xdg.Reload) // runs after Setenv restores the variable
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	xdg.Reload()
	return Options{Cwd: t.TempDir(), MCPName: "tutortop", MCPURL: "http://127.0.0.1:1/mcp/token", Instructions: "Be a tutor."}
}

// startFakeAntigravity starts the adapter on a fake agy, through a new
// session.
func startFakeAntigravity(t *testing.T, opts Options) (Session, *fakeCLI, *eventLog) {
	t.Helper()
	cli, started, sess, log := beginAntigravity(t, opts)
	acpReply(t, cli, acpExpect(t, cli, "session/new"), `{"sessionId":"s1","configOptions":`+antigravityConfigOptions+`}`)
	if err := wait(t, started); err != nil {
		t.Fatal(err)
	}
	return *sess, cli, log
}

// beginAntigravity starts the adapter on a fake agy, up to the point where
// it opens a session. *sess is set once started is done.
func beginAntigravity(t *testing.T, opts Options) (cli *fakeCLI, started <-chan error, sess *Session, log *eventLog) {
	t.Helper()
	clis := fakeBinary(t, "agy")
	var s Session
	log, sess = &eventLog{}, &s
	started = async(func() (err error) {
		*sess, err = startAntigravity(t.Context(), opts, log.add)
		if err == nil {
			t.Cleanup(func() { _ = (*sess).Close() })
		}
		return err
	})
	cli = clis.next(t)
	req := acpExpect(t, cli, "initialize")
	for _, c := range []string{"terminal", "fs.readTextFile", "fs.writeTextFile"} {
		if v := req.get("params.clientCapabilities." + c); v != nil && v != false {
			t.Errorf("initialize offers %s, want no file or terminal access", c)
		}
	}
	acpReply(t, cli, req, antigravityInitialize)
	return cli, started, sess, log
}

// acpExpect returns the adapter's next message, which must be a request for
// method.
func acpExpect(t *testing.T, cli *fakeCLI, method string) message {
	t.Helper()
	m := cli.recv(t)
	if m.str("method") != method || m.get("id") == nil {
		t.Fatalf("sent %v, want a %s request", m, method)
	}
	return m
}

func acpReply(t *testing.T, cli *fakeCLI, req message, result string) {
	t.Helper()
	id, _ := json.Marshal(req.get("id"))
	cli.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, id, result))
}

func acpReplyError(t *testing.T, cli *fakeCLI, req message, code int, msg string) {
	t.Helper()
	cli.send(t, map[string]any{"jsonrpc": "2.0", "id": req.get("id"), "error": map[string]any{"code": code, "message": msg}})
}

func decodeMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return m
}
