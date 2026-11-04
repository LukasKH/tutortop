package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/text"
)

// codexSession speaks the `codex app-server` JSON-RPC protocol, the same
// interface T3 Code and Codex's own rich clients use. It runs the learner's
// installed `codex` with its existing login.
type codexSession struct {
	*process
	rpc      *rpcConn
	cwd      string
	mcpName  string
	threadID string
	effort   string
	onEvent  func(Event)

	mu        sync.Mutex
	turnID    string
	turnDone  chan codexTurn
	filePaths map[string][]string // file-change item id -> paths, for approvals
}

type codexTurn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// codexTurnParams is both turn/start's result and turn/completed's params.
type codexTurnParams struct {
	Turn codexTurn `json:"turn"`
}

func startCodex(ctx context.Context, opts Options, onEvent func(Event)) (Session, error) {
	// The tutor gets the same rights as with Claude: it can write in the
	// workspace but not under tasks/ (the app manages those), and only read
	// elsewhere. A narrower non-write entry carves a read-only hole into a
	// writable folder; checked with `codex sandbox`.
	permissions := fmt.Sprintf(`permissions.tutortop.filesystem={":root"="read",%q="write",%q="read"}`,
		opts.Cwd, filepath.Join(opts.Cwd, "tasks"))
	s := &codexSession{cwd: opts.Cwd, mcpName: opts.MCPName, effort: opts.Effort, onEvent: onEvent, filePaths: map[string][]string{}}
	var err error
	s.process, s.rpc, err = startCodexServer(ctx, opts.Cwd, s.notify, s.request,
		"-c", `default_permissions="tutortop"`, "-c", permissions)
	if err != nil {
		return nil, err
	}

	type accountResult struct {
		RequiresOpenaiAuth bool `json:"requiresOpenaiAuth"`
		Account            any  `json:"account"`
	}
	if account, err := setupCall[accountResult](ctx, s.rpc, "account/read", map[string]any{}); err == nil && account.RequiresOpenaiAuth && account.Account == nil {
		return nil, s.abort(fmt.Errorf("not signed in to Codex: run `codex login` in a terminal, then try again"))
	}

	params := map[string]any{
		"cwd": opts.Cwd,
		// "untrusted" makes Codex ask before running commands or writing
		// outside the writable folders; request() declines both. The
		// filesystem limits come from the permission profile above.
		"approvalPolicy":        "untrusted",
		"developerInstructions": opts.Instructions,
		"config": map[string]any{
			"mcp_servers." + opts.MCPName: map[string]any{
				"url":              opts.MCPURL,
				"tool_timeout_sec": 600,
			},
		},
	}
	if opts.Model != "" {
		params["model"] = opts.Model
	}
	type threadResult struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	var res threadResult
	if opts.ResumeID != "" {
		p := map[string]any{"threadId": opts.ResumeID, "excludeTurns": true}
		maps.Copy(p, params)
		res, err = setupCall[threadResult](ctx, s.rpc, "thread/resume", p)
	}
	if opts.ResumeID == "" || err != nil {
		if res, err = setupCall[threadResult](ctx, s.rpc, "thread/start", params); err != nil {
			return nil, s.abort(err)
		}
	}
	s.threadID = res.Thread.ID
	return s, nil
}

// startCodexServer starts `codex app-server` and completes its handshake.
func startCodexServer(ctx context.Context, cwd string,
	onNotify func(method string, params json.RawMessage),
	onRequest func(method string, params json.RawMessage) (any, error),
	args ...string,
) (*process, *rpcConn, error) {
	proc, err := startProcess(ctx, cwd, nil, "codex", append([]string{"app-server"}, args...)...)
	if err != nil {
		return nil, nil, err
	}
	rpc := newRPCConn(proc.stdout, proc.send, onNotify, onRequest)
	if _, err := setupCall[json.RawMessage](ctx, rpc, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "tutortop", "title": "Tutortop", "version": "0.1.1"},
		"capabilities": nil,
	}); err != nil {
		return nil, nil, proc.abort(err)
	}
	_ = rpc.Notify("initialized", nil)
	return proc, rpc, nil
}

// setupCall is a call made while starting up, with its own deadline so a
// hung app-server can't block the app forever.
func setupCall[T any](ctx context.Context, rpc *rpcConn, method string, params any) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return rpc.Call[T](ctx, method, params)
}

func (s *codexSession) ID() string { return s.threadID }

func (s *codexSession) Prompt(ctx context.Context, text string) error {
	done := make(chan codexTurn, 1)
	s.mu.Lock()
	s.turnDone = done
	s.mu.Unlock()

	params := map[string]any{
		"threadId": s.threadID,
		"input":    []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}},
	}
	if s.effort != "" {
		params["effort"] = s.effort
	}
	res, err := s.rpc.Call[codexTurnParams](ctx, "turn/start", params)
	if err != nil {
		if s.Closed() {
			return s.exitError()
		}
		return err
	}
	s.mu.Lock()
	s.turnID = res.Turn.ID
	s.mu.Unlock()

	t, err := s.await(ctx, done)
	if err != nil {
		return err
	}
	if t.Status == "failed" && t.Error != nil {
		return fmt.Errorf("turn failed in Codex: %s", t.Error.Message)
	}
	return nil
}

func (s *codexSession) Cancel() {
	s.mu.Lock()
	turnID := s.turnID
	s.mu.Unlock()
	if turnID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = s.rpc.Call[json.RawMessage](ctx, "turn/interrupt", map[string]any{"threadId": s.threadID, "turnId": turnID})
	}()
}

type codexItem struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Status  string          `json:"status"`
	Server  string          `json:"server"`
	Tool    string          `json:"tool"`
	Command json.RawMessage `json:"command"`
	Query   string          `json:"query"`
	Changes []struct {
		Path string `json:"path"`
	} `json:"changes"`
}

func (it codexItem) paths() []string {
	var paths []string
	for _, c := range it.Changes {
		paths = append(paths, c.Path)
	}
	return paths
}

func (s *codexSession) notify(method string, params json.RawMessage) {
	switch method {
	case "item/agentMessage/delta":
		if p, err := decode[struct {
			Delta string `json:"delta"`
		}](params); err == nil {
			s.onEvent(Event{Kind: EventText, Text: p.Delta})
		}
	case "item/started", "item/completed":
		p, err := decode[struct {
			Item codexItem `json:"item"`
		}](params)
		if err != nil {
			return
		}
		// v2 file-change approvals only carry the item id, so remember
		// which files each change touches.
		if p.Item.Type == "fileChange" {
			s.mu.Lock()
			if method == "item/started" {
				s.filePaths[p.Item.ID] = p.Item.paths()
			} else {
				delete(s.filePaths, p.Item.ID)
			}
			s.mu.Unlock()
		}
		t, ok := codexTool(p.Item)
		if !ok {
			return
		}
		t.ID, t.Status = p.Item.ID, ToolRunning
		if method == "item/completed" {
			t.Status = ToolDone
			if p.Item.Status == "failed" || p.Item.Status == "declined" {
				t.Status = ToolFailed
			}
		}
		s.onEvent(Event{Kind: EventTool, Tool: t})
	case "turn/completed":
		p, err := decode[codexTurnParams](params)
		if err != nil {
			return
		}
		s.mu.Lock()
		ch := s.turnDone
		s.turnDone, s.turnID = nil, ""
		s.mu.Unlock()
		if ch != nil {
			ch <- p.Turn
		}
	}
}

// codexTool describes an item that is a tool call. Other items, like the
// agent's messages, aren't.
func codexTool(it codexItem) (Tool, bool) {
	switch it.Type {
	case "mcpToolCall":
		return Tool{Kind: ToolOther, Title: it.Server + " · " + it.Tool, MCPServer: it.Server, MCPTool: it.Tool}, true
	case "commandExecution":
		cmd, err := decode[string](it.Command)
		if err != nil {
			parts, _ := decode[[]string](it.Command)
			cmd = strings.Join(parts, " ")
		}
		return Tool{Kind: ToolExecute, Title: "Ran " + text.Truncate(strings.ReplaceAll(cmd, "\n", " "), 80)}, true
	case "fileChange":
		return Tool{Kind: ToolEdit, Title: "Edited " + text.Truncate(strings.Join(it.paths(), ", "), 80), Paths: it.paths()}, true
	case "webSearch":
		return Tool{Kind: ToolFetch, Title: "Searched the web" + optional(": ", it.Query)}, true
	}
	return Tool{}, false
}

// request answers Codex's approval requests: commands are always declined,
// file changes go through allowTool. The v1 names (execCommandApproval,
// applyPatchApproval) are what older app-servers send.
func (s *codexSession) request(method string, params json.RawMessage) (any, error) {
	switch method {
	case "item/commandExecution/requestApproval":
		return map[string]any{"decision": "decline"}, nil
	case "execCommandApproval":
		return map[string]any{"decision": map[string]any{"denied": map[string]any{"rejection": errNoShell.Error()}}}, nil

	case "item/fileChange/requestApproval":
		p, _ := decode[struct {
			ItemID string `json:"itemId"`
		}](params)
		s.mu.Lock()
		paths := s.filePaths[p.ItemID]
		s.mu.Unlock()
		if allowTool(s.cwd, ToolEdit, paths) != nil {
			return map[string]any{"decision": "decline"}, nil
		}
		return map[string]any{"decision": "accept"}, nil
	case "applyPatchApproval":
		p, _ := decode[struct {
			FileChanges map[string]json.RawMessage `json:"fileChanges"`
		}](params)
		if err := allowTool(s.cwd, ToolEdit, slices.Collect(maps.Keys(p.FileChanges))); err != nil {
			return map[string]any{"decision": map[string]any{"denied": map[string]any{"rejection": err.Error()}}}, nil
		}
		return map[string]any{"decision": "approved"}, nil

	case "mcpServer/elicitation/request":
		// Only our own server's requests are pre-approved.
		p, _ := decode[struct {
			ServerName string `json:"serverName"`
		}](params)
		action := "decline"
		if p.ServerName == s.mcpName {
			action = "accept"
		}
		return map[string]any{"action": action, "content": nil, "_meta": nil}, nil
	case "item/tool/requestUserInput":
		return map[string]any{"answers": map[string]any{}}, nil
	}
	return nil, fmt.Errorf("tutortop doesn't handle %s", method)
}
