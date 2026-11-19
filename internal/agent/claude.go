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

	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/LukasKH/tutortop/internal/text"
)

// claudeSession drives the learner's own, unmodified `claude` binary over the
// stream-json protocol, the same interface the official Claude Agent SDK uses.
// Authentication is whatever the learner's Claude Code is signed in with.
type claudeSession struct {
	*process

	id      string
	policy  policy
	mcpName string
	onEvent func(Event)

	reqID       atomic.Int64
	interrupted atomic.Bool
	checkedMCP  atomic.Bool

	mu      sync.Mutex
	result  chan claudeResult
	pending map[string]chan claudeControlResponse // our control requests awaiting a reply
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
		"--permission-prompt-tool", "stdio",
	}
	args = append(append(args, "--disallowedTools"), claudeDisallowed(opts)...)
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

	proc, err := startProcess(ctx, opts.Cwd, []string{
		// create_task runs the sandbox checks, which can take minutes.
		"MCP_TOOL_TIMEOUT=600000",
		// Nothing may outlive the turn, while the app shows the tutor as
		// done: Claude waits for subagents, and doesn't move a long MCP call
		// such as create_task to the background either.
		"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1",
	}, "claude", args...)
	if err != nil {
		return nil, err
	}
	s := &claudeSession{
		process: proc,
		id:      sessionID,
		policy:  newPolicy(opts),
		mcpName: opts.MCPName,
		onEvent: onEvent,
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
	if r.Subtype == claudeError {
		return errors.New(cmp.Or(r.Error, "control request failed"))
	}
	return nil
}

func (s *claudeSession) readLoop() {
	sc := bufio.NewScanner(s.stdout)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		if l, err := decode.JSON[claudeLine](sc.Bytes()); err == nil {
			s.handle(l)
		}
	}
}

// handle acts on one line of Claude Code's stream-json output.
func (s *claudeSession) handle(l claudeLine) {
	switch l.Type {
	case "system":
		if l.Subtype == "init" {
			s.checkMCP(l)
		}
	case "stream_event":
		s.streamText(l)
	case "assistant":
		s.toolsStarted(l)
	case "user":
		s.toolsFinished(l)
	case "result":
		s.deliverResult(l.claudeResult)
	case "control_response":
		s.deliverControlResponse(l.Response)
	case "control_request":
		go s.answerControl(l.RequestID, l.Request)
		// control_cancel_request needs nothing: we answer permission
		// requests at once, so there's never one in flight to cancel.
	}
}

// streamText passes on a piece of the reply's text.
func (s *claudeSession) streamText(l claudeLine) {
	ev, err := decode.JSON[struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	}](l.Event)
	// A subagent's text is its report to the tutor, not the reply.
	if err == nil && l.ParentToolUseID == "" && ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" {
		s.onEvent(Event{Kind: EventText, Text: ev.Delta.Text})
	}
}

// toolsStarted reports the tool calls in an assistant message as running.
func (s *claudeSession) toolsStarted(l claudeLine) {
	for _, b := range blocks(l.Message.Content) {
		if b.Type == "tool_use" {
			t := claudeTool(b.Name, b.Input)
			t.ID, t.Status = b.ID, ToolRunning
			s.onEvent(Event{Kind: EventTool, Tool: t})
		}
	}
}

// toolsFinished reports the tool results in a user message as done or failed.
func (s *claudeSession) toolsFinished(l claudeLine) {
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
}

// deliverResult hands a turn's result to the prompt waiting for it, if any.
func (s *claudeSession) deliverResult(r claudeResult) {
	s.mu.Lock()
	ch := s.result
	s.result = nil
	s.mu.Unlock()
	if ch != nil {
		ch <- r
	}
}

// deliverControlResponse hands a reply to the control request waiting for
// it, if any.
func (s *claudeSession) deliverControlResponse(r claudeControlResponse) {
	s.mu.Lock()
	ch := s.pending[r.RequestID]
	s.mu.Unlock()
	if ch != nil {
		ch <- r
	}
}

// checkMCP warns once if our MCP server didn't connect: without it the tutor
// can talk but can't create tasks or show questions.
func (s *claudeSession) checkMCP(l claudeLine) {
	if s.checkedMCP.Load() {
		return
	}
	status := claudeMCPMissing
	for _, m := range l.MCPServers {
		if m.Name == s.mcpName {
			status = m.Status
		}
	}
	switch status {
	case claudeMCPConnected:
		s.checkedMCP.Store(true)
		return
	case claudeMCPPending:
		return // still connecting; the next init message will tell
	case claudeMCPMissing:
	}
	// Missing, failed, or anything else.
	s.checkedMCP.Store(true)
	msg := fmt.Sprintf("The tutor couldn't connect to Tutortop's tools (status: %s), so it can't create tasks or ask questions. Restart the app and try again.", status)
	s.onEvent(Event{Kind: EventWarning, Text: msg})
}

// answerControl handles Claude Code's requests to us. Permission requests
// (can_use_tool) get our policy; anything else is unsupported.
func (s *claudeSession) answerControl(id string, raw json.RawMessage) {
	_ = s.send(map[string]any{"type": "control_response", "response": s.controlReply(id, raw)})
}

// controlReply is our answer to one of Claude Code's control requests.
func (s *claudeSession) controlReply(id string, raw json.RawMessage) map[string]any {
	req, _ := decode.JSON[struct {
		Subtype  string          `json:"subtype"`
		ToolName string          `json:"tool_name"`
		Input    json.RawMessage `json:"input"`
	}](raw)
	if req.Subtype != "can_use_tool" {
		return map[string]any{"subtype": claudeError, "request_id": id, "error": "Tutortop doesn't support " + req.Subtype}
	}
	response := map[string]any{"behavior": "allow", "updatedInput": req.Input}
	if err := s.canUseTool(req.ToolName, req.Input); err != nil {
		response = map[string]any{"behavior": "deny", "message": err.Error()}
	}
	return map[string]any{"subtype": claudeSuccess, "request_id": id, "response": response}
}

// canUseTool decides tool uses Claude Code asks about: Claude's own tools
// outside claudeAllowed are refused, the rest go to the policy. Edits under
// tasks/ never get here: the deny rule in startClaude refuses them first.
// Paths outside the workspace do get here, because acceptEdits only covers
// the workspace.
func (s *claudeSession) canUseTool(name string, input json.RawMessage) error {
	t := claudeTool(name, input)
	if t.MCPServer == "" && !t.Subagent && !claudeAllowed[name] {
		return errUnavailable
	}
	return s.policy.allow(t)
}

type claudeLine struct {
	claudeResult

	Type  string          `json:"type"`
	Event json.RawMessage `json:"event"`
	// ParentToolUseID is set on a subagent's messages: the Agent call that
	// started it.
	ParentToolUseID string `json:"parent_tool_use_id"`
	Message         struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	RequestID  string                `json:"request_id"`
	Request    json.RawMessage       `json:"request"`
	Response   claudeControlResponse `json:"response"`
	MCPServers []struct {
		Name   string          `json:"name"`
		Status claudeMCPStatus `json:"status"`
	} `json:"mcp_servers"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

func blocks(raw json.RawMessage) []claudeBlock {
	bs, _ := decode.JSON[[]claudeBlock](raw) // content can also be a plain string; ignore that
	return bs
}

type claudeResult struct {
	IsError bool   `json:"is_error"`
	Subtype string `json:"subtype"`
	Result  string `json:"result"`
}

type claudeControlResponse struct {
	Subtype   claudeReply `json:"subtype"`
	RequestID string      `json:"request_id"`
	Error     string      `json:"error"`
}

// claudeReply is how a control request went, in either direction.
type claudeReply string

const (
	claudeSuccess claudeReply = "success"
	claudeError   claudeReply = "error"
)

// claudeMCPStatus is how an MCP server's connection is going.
type claudeMCPStatus string

const (
	claudeMCPConnected claudeMCPStatus = "connected"
	claudeMCPPending   claudeMCPStatus = "pending"
	claudeMCPMissing   claudeMCPStatus = "missing" // not in the list at all
)

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
		if !e.IsDir() || !strings.HasPrefix(e.Name(), name[:claudeMaxNameLen]+"-") {
			continue
		}
		if _, err := os.Stat(filepath.Join(projects, e.Name(), id+".jsonl")); err == nil {
			return true
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

// claudeDisallowed are the tools Claude Code refuses by itself: the shell,
// edits under tasks/ (which acceptEdits would otherwise approve), and
// subagents (Agent, formerly Task) unless the session allows them.
func claudeDisallowed(opts Options) []string {
	tools := []string{"Bash", "BashOutput", "KillShell", "Edit(./tasks/**)"}
	if !opts.Subagents {
		tools = append(tools, "Agent", "Task")
	}
	return tools
}

// Tools the tutor may use without asking. Everything else is denied: the tutor
// works through the tutortop MCP tools, not the shell.
var claudeAllowed = map[string]bool{
	"Read":      true,
	"Write":     true,
	"Edit":      true,
	"MultiEdit": true,
	"Glob":      true,
	"Grep":      true,
	"WebSearch": true,
	"WebFetch":  true,
	"TodoWrite": true,
}

// claudeTool describes a call to one of Claude Code's tools.
func claudeTool(name string, input json.RawMessage) Tool {
	// The fields that decode survive an odd value in another, so the
	// permission check still sees the path.
	in, _ := decode.JSON[struct {
		FilePath    string `json:"file_path"`
		Path        string `json:"path"`
		Pattern     string `json:"pattern"`
		Query       string `json:"query"`
		URL         string `json:"url"`
		Description string `json:"description"`
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
	case "Agent", "Task":
		t.Subagent, t.Title = true, "Researching"+optional(": ", in.Description)
	default:
		if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
			t.MCPServer, t.MCPTool, _ = strings.Cut(rest, "__")
			t.Title = t.MCPServer + " · " + t.MCPTool
		}
	}
	return t
}

func claudeModels(context.Context) ([]Model, error) { return claudeModelList, nil }

// Claude Code takes model aliases that always point at the latest model of a
// family, so this list doesn't go stale with each release.
var claudeModelList = []Model{
	{ID: "", Name: "Default", Description: "Whatever Claude Code is set to use", Efforts: claudeEfforts},
	{ID: "fable", Name: "Fable", Description: "Most capable", Efforts: claudeEfforts, DefaultEffort: "medium"},
	{ID: "opus", Name: "Opus", Description: "Very capable, for complex work", Efforts: claudeEfforts, DefaultEffort: "medium"},
	{ID: "sonnet", Name: "Sonnet", Description: "Fast and capable for everyday work", Efforts: claudeEfforts, DefaultEffort: "medium"},
	{ID: "haiku", Name: "Haiku", Description: "Fastest and lightest", Efforts: claudeEfforts, DefaultEffort: "medium"},
}

// Every current model takes the same efforts (as listed by `claude --help`),
// defaulting to medium.
var claudeEfforts = []Effort{
	{"low", "Fastest, light reasoning"},
	{"medium", "Balanced"},
	{"high", "Deeper reasoning"},
	{"xhigh", "Extra deep reasoning"},
	{"max", "Deepest reasoning, slowest"},
}
