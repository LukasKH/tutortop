package agent

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/LukasKH/tutortop/internal/text"
)

// claudeSession drives the learner's own, unmodified `claude` binary over the
// stream-json protocol, the same interface the official Claude Agent SDK uses.
// Authentication is whatever the learner's Claude Code is signed in with.
type claudeSession struct {
	*process
	id        string
	cwd       string
	mcpName   string
	mcpPrefix string
	onEvent   func(Event)

	reqID       atomic.Int64
	interrupted atomic.Bool
	checkedMCP  atomic.Bool

	mu      sync.Mutex
	result  chan claudeResult
	pending map[string]chan claudeControlResponse // our control requests awaiting a reply
}

type claudeResult struct {
	IsError bool   `json:"is_error"`
	Subtype string `json:"subtype"`
	Result  string `json:"result"`
}

type claudeControlResponse struct {
	Subtype   string `json:"subtype"` // success | error
	RequestID string `json:"request_id"`
	Error     string `json:"error"`
}

// Tools the coach may use without asking. Everything else is denied: the coach
// works through the tutortop MCP tools, not the shell.
var claudeAllowed = map[string]bool{
	"Read": true, "Write": true, "Edit": true, "MultiEdit": true, "Glob": true, "Grep": true,
	"WebSearch": true, "WebFetch": true, "TodoWrite": true,
}

func startClaude(ctx context.Context, opts Options, onEvent func(Event)) (Session, error) {
	mcpConfig, _ := json.Marshal(map[string]any{
		"mcpServers": map[string]any{opts.MCPName: map[string]any{"type": "http", "url": opts.MCPURL}},
	})
	args := []string{
		"--output-format", "stream-json", "--verbose",
		"--input-format", "stream-json",
		"--include-partial-messages",
		"--append-system-prompt", opts.Instructions,
		"--mcp-config", string(mcpConfig), "--strict-mcp-config",
		// acceptEdits approves edits inside the workspace before our
		// can_use_tool callback runs, so the tasks/ rule must be a deny rule:
		// deny rules beat the mode and any allow rules in the learner's own
		// settings. Edit rules cover every file-editing tool.
		"--permission-mode", "acceptEdits",
		"--allowedTools", "mcp__" + opts.MCPName + "__*",
		// Subagents (Agent, formerly Task) can run in the background and
		// keep working after the turn ends, while the app shows the coach
		// as done; the coach does its work in the turn instead.
		"--disallowedTools", "Bash", "BashOutput", "KillShell", "Agent", "Task", "Edit(./tasks/**)",
		"--permission-prompt-tool", "stdio",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Effort != "" {
		args = append(args, "--effort", opts.Effort)
	}
	sessionID := opts.ResumeID
	if sessionID != "" && claudeSessionExists(opts.Cwd, sessionID) {
		args = append(args, "--resume", sessionID)
	} else {
		sessionID = uuid.New().String()
		args = append(args, "--session-id", sessionID)
	}

	proc, err := startProcess(ctx, opts.Cwd,
		// create_task runs the sandbox checks, which can take minutes.
		[]string{"MCP_TOOL_TIMEOUT=600000"},
		"claude", args...)
	if err != nil {
		return nil, err
	}
	s := &claudeSession{
		process: proc, id: sessionID, cwd: opts.Cwd, onEvent: onEvent,
		mcpName: opts.MCPName, mcpPrefix: "mcp__" + opts.MCPName + "__",
		pending: map[string]chan claudeControlResponse{},
	}
	go s.readLoop()

	// The SDKs wait for the initialize reply before sending anything else.
	initCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := s.request(initCtx, map[string]any{"subtype": "initialize", "hooks": nil}); err != nil {
		return nil, fmt.Errorf("starting Claude Code: %w", proc.abort(err))
	}
	return s, nil
}

func (s *claudeSession) ID() string { return s.id }

// claudeSessionExists checks Claude Code's transcript store, so a deleted or
// foreign session id starts a fresh session instead of failing. The folder
// naming follows the CLI, as mirrored in the official Python SDK
// (claude_agent_sdk/_internal/sessions.py).
func claudeSessionExists(cwd, id string) bool {
	base := os.Getenv("CLAUDE_CONFIG_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		base = filepath.Join(home, ".claude")
	}
	projects := filepath.Join(base, "projects")
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}

	name := claudeProjectName(cwd)
	if len(name) <= claudeMaxNameLen {
		_, err := os.Stat(filepath.Join(projects, name, id+".jsonl"))
		return err == nil
	}
	// Long names end in a hash the SDKs can't reproduce exactly; match on the
	// prefix, like the Python SDK does.
	entries, _ := os.ReadDir(projects)
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), name[:claudeMaxNameLen]+"-") {
			if _, err := os.Stat(filepath.Join(projects, e.Name(), id+".jsonl")); err == nil {
				return true
			}
		}
	}
	return false
}

const claudeMaxNameLen = 200

// claudeProjectName replaces every character except ASCII letters and digits
// with "-", as the CLI does when naming a project's session folder.
func claudeProjectName(path string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, path)
}

// request sends a control request and waits for Claude Code's reply.
func (s *claudeSession) request(ctx context.Context, req map[string]any) error {
	id := fmt.Sprintf("req_%d", s.reqID.Add(1))
	reply := make(chan claudeControlResponse, 1)
	s.mu.Lock()
	s.pending[id] = reply
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	if err := s.send(map[string]any{"type": "control_request", "request_id": id, "request": req}); err != nil {
		return err
	}
	r, err := s.await(ctx, reply)
	if err != nil {
		return err
	}
	if r.Subtype == "error" {
		return errors.New(cmp.Or(r.Error, "control request failed"))
	}
	return nil
}

func (s *claudeSession) Prompt(ctx context.Context, text string) error {
	done := make(chan claudeResult, 1)
	s.mu.Lock()
	s.result = done
	s.mu.Unlock()
	s.interrupted.Store(false)
	err := s.send(map[string]any{
		"type":               "user",
		"message":            map[string]any{"role": "user", "content": text},
		"parent_tool_use_id": nil,
		"session_id":         "",
	})
	if err != nil {
		return s.exitError()
	}
	r, err := s.await(ctx, done)
	if err != nil {
		return err
	}
	if r.IsError && !s.interrupted.Load() {
		return fmt.Errorf("turn failed in Claude Code: %s", cmp.Or(r.Result, r.Subtype, "the turn ended with an error"))
	}
	return nil
}

// Cancel interrupts the current turn. Claude Code then ends the turn with
// an error result, which Prompt treats as a normal stop.
func (s *claudeSession) Cancel() {
	s.interrupted.Store(true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.request(ctx, map[string]any{"subtype": "interrupt"})
	}()
}

type claudeLine struct {
	Type    string          `json:"type"`
	Event   json.RawMessage `json:"event"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	RequestID  string                `json:"request_id"`
	Request    json.RawMessage       `json:"request"`
	Response   claudeControlResponse `json:"response"`
	MCPServers []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"mcp_servers"`
	claudeResult
}

type claudeBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

func (s *claudeSession) readLoop() {
	sc := bufio.NewScanner(s.stdout)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		l, err := decode[claudeLine](sc.Bytes())
		if err != nil {
			continue
		}
		switch l.Type {
		case "system":
			if l.Subtype == "init" {
				s.checkMCP(l)
			}
		case "stream_event":
			ev, err := decode[struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}](l.Event)
			if err == nil && ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" {
				s.onEvent(Event{Kind: EventText, Text: ev.Delta.Text})
			}
		case "assistant":
			for _, b := range blocks(l.Message.Content) {
				if b.Type == "tool_use" {
					t := claudeTool(b.Name, b.Input)
					t.ID, t.Status = b.ID, ToolRunning
					s.onEvent(Event{Kind: EventTool, Tool: t})
				}
			}
		case "user":
			for _, b := range blocks(l.Message.Content) {
				if b.Type != "tool_result" {
					continue
				}
				status := ToolDone
				if b.IsError {
					status = ToolFailed
				}
				s.onEvent(Event{Kind: EventTool, Tool: Tool{ID: b.ToolUseID, Status: status}})
			}
		case "result":
			s.mu.Lock()
			ch := s.result
			s.result = nil
			s.mu.Unlock()
			if ch != nil {
				ch <- l.claudeResult
			}
		case "control_response":
			s.mu.Lock()
			ch := s.pending[l.Response.RequestID]
			s.mu.Unlock()
			if ch != nil {
				ch <- l.Response
			}
		case "control_request":
			go s.answerControl(l.RequestID, l.Request)
			// control_cancel_request needs nothing: we answer permission
			// requests at once, so there's never one in flight to cancel.
		}
	}
}

// checkMCP warns once if our MCP server didn't connect: without it the coach
// can talk but can't create tasks or show questions.
func (s *claudeSession) checkMCP(l claudeLine) {
	if s.checkedMCP.Load() {
		return
	}
	status := "missing"
	for _, m := range l.MCPServers {
		if m.Name == s.mcpName {
			status = m.Status
		}
	}
	switch status {
	case "connected":
		s.checkedMCP.Store(true)
	case "pending":
		// Still connecting; the next init message will tell.
	default:
		s.checkedMCP.Store(true)
		s.onEvent(Event{Kind: EventWarning, Text: fmt.Sprintf(
			"The coach couldn't connect to Tutortop's tools (status: %s), so it can't create tasks or ask questions. Restart the app and try again.", status)})
	}
}

func blocks(raw json.RawMessage) []claudeBlock {
	bs, _ := decode[[]claudeBlock](raw) // content can also be a plain string; ignore that
	return bs
}

// answerControl handles Claude Code's requests to us. Permission requests
// (can_use_tool) get our policy; anything else is unsupported.
func (s *claudeSession) answerControl(id string, raw json.RawMessage) {
	req, _ := decode[struct {
		Subtype  string          `json:"subtype"`
		ToolName string          `json:"tool_name"`
		Input    json.RawMessage `json:"input"`
	}](raw)
	reply := map[string]any{"subtype": "success", "request_id": id}
	if req.Subtype == "can_use_tool" {
		if err := s.canUseTool(req.ToolName, req.Input); err != nil {
			reply["response"] = map[string]any{"behavior": "deny", "message": err.Error()}
		} else {
			reply["response"] = map[string]any{"behavior": "allow", "updatedInput": req.Input}
		}
	} else {
		reply["subtype"] = "error"
		reply["error"] = "Tutortop doesn't support " + req.Subtype
	}
	_ = s.send(map[string]any{"type": "control_response", "response": reply})
}

// canUseTool decides tool uses Claude Code asks about. Edits under tasks/
// never get here: the deny rule in startClaude refuses them first. Paths
// outside the workspace do get here, because acceptEdits only covers the
// workspace.
func (s *claudeSession) canUseTool(name string, input json.RawMessage) error {
	if strings.HasPrefix(name, s.mcpPrefix) {
		return nil
	}
	if !claudeAllowed[name] {
		return errors.New("not available in Tutortop. Use the tutortop tools: create_task runs code in the sandbox")
	}
	t := claudeTool(name, input)
	return allowTool(s.cwd, t.Kind, t.Paths)
}

// claudeTool describes a call to one of Claude Code's tools.
func claudeTool(name string, input json.RawMessage) Tool {
	in, _ := decode[struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Pattern  string `json:"pattern"`
		Query    string `json:"query"`
		URL      string `json:"url"`
	}](input)
	t := Tool{Kind: ToolOther, Title: name}
	for _, p := range []string{in.FilePath, in.Path} {
		if p != "" {
			t.Paths = append(t.Paths, p)
		}
	}
	switch name {
	case "Read":
		t.Kind, t.Title = ToolRead, "Read "+filepath.Base(in.FilePath)
	case "Write", "Edit", "MultiEdit":
		t.Kind, t.Title = ToolEdit, "Edited "+filepath.Base(in.FilePath)
	case "Glob", "Grep":
		t.Kind, t.Title = ToolSearch, "Searched files"+optional(": ", in.Pattern)
	case "WebSearch":
		t.Kind, t.Title = ToolFetch, "Searched the web"+optional(": ", in.Query)
	case "WebFetch":
		t.Kind, t.Title = ToolFetch, "Fetched "+text.Truncate(in.URL, 80)
	case "TodoWrite", "ToolSearch":
		t.Kind = ToolThink
	default:
		if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
			t.MCPServer, t.MCPTool, _ = strings.Cut(rest, "__")
			t.Title = t.MCPServer + " · " + t.MCPTool
		}
	}
	return t
}
