package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/LukasKH/tutortop/internal/text"
)

// codexSession speaks the `codex app-server` JSON-RPC protocol, the same
// interface T3 Code and Codex's own rich clients use. It runs the learner's
// installed `codex` with its existing login.
type codexSession struct {
	*process

	rpc      *rpcConn
	policy   policy
	threadID string
	effort   string
	onEvent  func(Event)

	mu       sync.Mutex
	turnID   string
	turnDone chan codexTurn
	// cancelled is a Cancel that came before Codex named the turn; Prompt
	// interrupts the turn once it has its id.
	cancelled bool
	filePaths map[string][]string // file-change item id -> paths, for approvals
}

func startCodex(ctx context.Context, opts Options, onEvent func(Event)) (Session, error) {
	// The tutor gets the same rights as with Claude: it can write in the
	// workspace but not under tasks/ (the app manages those) or the settings
	// in configPaths, and only read elsewhere. A narrower non-write entry
	// carves a read-only hole into a writable folder; checked with `codex sandbox`.
	filesystem := []string{`":root"="read"`, fmt.Sprintf(`%q="write"`, opts.Cwd)}
	for _, rel := range append([]string{"tasks"}, configPaths...) {
		filesystem = append(filesystem, fmt.Sprintf(`%q="read"`, filepath.Join(opts.Cwd, rel)))
	}
	permissions := "permissions.tutortop.filesystem={" + strings.Join(filesystem, ",") + "}"
	s := &codexSession{policy: newPolicy(opts), effort: opts.Effort, onEvent: onEvent, filePaths: map[string][]string{}}
	var err error
	s.process, s.rpc, err = startCodexServer(ctx, opts.Binary, opts.Cwd, s.notify, s.request,
		"-c", `default_permissions="tutortop"`, "-c", permissions)
	if err != nil {
		return nil, err
	}

	type accountResult struct {
		RequiresOpenaiAuth bool `json:"requiresOpenaiAuth"`
		Account            any  `json:"account"`
	}
	if account, err := setupCall[accountResult](ctx, s.rpc, "account/read", map[string]any{}); err == nil && account.RequiresOpenaiAuth && account.Account == nil {
		return nil, s.abort(fmt.Errorf("not signed in to Codex: run `%s login` in a terminal, then try again", opts.Binary))
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
				"tool_timeout_sec": 1800,
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

func (s *codexSession) ID() string { return s.threadID }

func (s *codexSession) Prompt(ctx context.Context, text string) error {
	done := make(chan codexTurn, 1)
	s.mu.Lock()
	s.turnDone, s.cancelled = done, false
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
	running := s.turnDone == done // turn/completed may already have come
	if running {
		s.turnID = res.Turn.ID
	}
	cancelled := s.cancelled
	s.mu.Unlock()
	if running && cancelled {
		s.interrupt(res.Turn.ID)
	}

	t, err := s.await(ctx, done)
	if err != nil {
		return err
	}
	if t.Status == codexFailed && t.Error != nil {
		return fmt.Errorf("turn failed in Codex: %s", t.Error.Message)
	}
	return nil
}

func (s *codexSession) Cancel() {
	s.mu.Lock()
	turnID := s.turnID
	if turnID == "" && s.turnDone != nil {
		s.cancelled = true // turn/start hasn't answered yet
	}
	s.mu.Unlock()
	if turnID != "" {
		s.interrupt(turnID)
	}
}

// interrupt asks Codex to end a turn, without waiting for the answer.
func (s *codexSession) interrupt(turnID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = s.rpc.Call[json.RawMessage](ctx, "turn/interrupt", map[string]any{"threadId": s.threadID, "turnId": turnID})
	}()
}

func (s *codexSession) notify(method string, params json.RawMessage) {
	switch method {
	case "item/agentMessage/delta":
		if p, err := decode.JSON[struct {
			Delta string `json:"delta"`
		}](params); err == nil {
			s.onEvent(Event{Kind: EventText, Text: p.Delta})
		}
	// Codex shows its reasoning as a summary, in parts that each start with
	// a bold title.
	case "item/reasoning/summaryTextDelta":
		if p, err := decode.JSON[struct {
			Delta string `json:"delta"`
		}](params); err == nil {
			s.onEvent(Event{Kind: EventThinking, Text: p.Delta})
		}
	case "item/reasoning/summaryPartAdded":
		if p, err := decode.JSON[struct {
			SummaryIndex int `json:"summaryIndex"`
		}](params); err == nil && p.SummaryIndex > 0 {
			s.onEvent(Event{Kind: EventThinking, Text: "\n\n"})
		}
	case "item/started", "item/completed":
		p, err := decode.JSON[struct {
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
			if p.Item.Status == codexFailed || p.Item.Status == codexDeclined {
				t.Status = ToolFailed
			}
		}
		s.onEvent(Event{Kind: EventTool, Tool: t})
	case "turn/completed":
		p, err := decode.JSON[codexTurnParams](params)
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

// request answers Codex's approval requests: commands are always declined,
// file changes and MCP elicitations go through the policy, and anything else
// gets an error, which Codex takes as a refusal. The v1 names (execCommandApproval,
// applyPatchApproval) are what older app-servers send.
func (s *codexSession) request(method string, params json.RawMessage) (any, error) {
	switch method {
	case "item/commandExecution/requestApproval":
		return map[string]any{"decision": "decline"}, nil
	case "execCommandApproval":
		return map[string]any{"decision": map[string]any{"denied": map[string]any{"rejection": errNoShell.Error()}}}, nil

	case "item/fileChange/requestApproval":
		p, _ := decode.JSON[struct {
			ItemID string `json:"itemId"`
		}](params)
		s.mu.Lock()
		paths := s.filePaths[p.ItemID]
		s.mu.Unlock()
		if s.policy.allow(Tool{Kind: ToolEdit, Paths: paths}) != nil {
			return map[string]any{"decision": "decline"}, nil
		}
		return map[string]any{"decision": "accept"}, nil
	case "applyPatchApproval":
		p, _ := decode.JSON[struct {
			FileChanges map[string]json.RawMessage `json:"fileChanges"`
		}](params)
		if err := s.policy.allow(Tool{Kind: ToolEdit, Paths: slices.Collect(maps.Keys(p.FileChanges))}); err != nil {
			return map[string]any{"decision": map[string]any{"denied": map[string]any{"rejection": err.Error()}}}, nil
		}
		return map[string]any{"decision": "approved"}, nil

	case "mcpServer/elicitation/request":
		// Only our own server's requests are pre-approved.
		p, _ := decode.JSON[struct {
			ServerName string `json:"serverName"`
		}](params)
		action := "decline"
		if s.policy.allow(Tool{Kind: ToolOther, MCPServer: p.ServerName}) == nil {
			action = "accept"
		}
		return map[string]any{"action": action, "content": nil, "_meta": nil}, nil
	case "item/tool/requestUserInput":
		return map[string]any{"answers": map[string]any{}}, nil
	}
	return nil, fmt.Errorf("tutortop doesn't handle %s", method)
}

type codexTurn struct {
	ID     string      `json:"id"`
	Status codexStatus `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// codexStatus is how a turn or item ended, the values the app looks for.
type codexStatus string

const (
	codexFailed   codexStatus = "failed"
	codexDeclined codexStatus = "declined"
)

// codexTurnParams is both turn/start's result and turn/completed's params.
type codexTurnParams struct {
	Turn codexTurn `json:"turn"`
}

type codexItem struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Status  codexStatus     `json:"status"`
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

// startCodexServer starts `codex app-server` and completes its handshake.
func startCodexServer(ctx context.Context, binary, cwd string,
	onNotify func(method string, params json.RawMessage),
	onRequest func(method string, params json.RawMessage) (any, error),
	args ...string,
) (*process, *rpcConn, error) {
	proc, err := startProcess(ctx, cwd, binary, nil, append([]string{"app-server"}, args...)...)
	if err != nil {
		return nil, nil, err
	}
	rpc := newRPCConn(proc, onNotify, onRequest)
	if _, err := setupCall[json.RawMessage](ctx, rpc, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "tutortop", "title": "Tutortop", "version": "0.2.6"}, // x-release-please-version
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

// codexTool describes an item that is a tool call. Other items, like the
// agent's messages, aren't.
func codexTool(it codexItem) (Tool, bool) {
	switch it.Type {
	case "mcpToolCall":
		return Tool{Kind: ToolOther, Title: it.Server + " · " + it.Tool, MCPServer: it.Server, MCPTool: it.Tool}, true
	case "commandExecution":
		cmd, err := decode.JSON[string](it.Command)
		if err != nil {
			parts, _ := decode.JSON[[]string](it.Command)
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

// codexModels asks a short-lived `codex app-server` for its model list, so
// the list follows the learner's account and CLI version.
func codexModels(ctx context.Context, binary string) ([]Model, error) {
	var models []Model
	err := withCodexServer(ctx, binary, func(ctx context.Context, rpc *rpcConn) error {
		var err error
		models, err = codexModelList(ctx, rpc)
		return err
	})
	return models, err
}

// codexInfo is what a short-lived `codex app-server` says.
type codexInfo struct {
	account string // "ChatGPT Plus", "API key"
	limits  []Limit
	models  []Model
}

// codexCheck asks a short-lived `codex app-server` for the account it's
// signed in with, its usage limits and the models it offers.
func codexCheck(ctx context.Context, binary string) (codexInfo, error) {
	var info codexInfo
	err := withCodexServer(ctx, binary, func(ctx context.Context, rpc *rpcConn) error {
		var err error
		if info.account, info.limits, err = codexAccount(ctx, rpc, binary); err != nil {
			return err
		}
		info.models, err = codexModelList(ctx, rpc)
		return err
	})
	return info, err
}

// withCodexServer starts a short-lived `codex app-server`, asks it what ask
// wants to know, and stops it.
func withCodexServer(ctx context.Context, binary string, ask func(ctx context.Context, rpc *rpcConn) error) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	proc, rpc, err := startCodexServer(ctx, binary, os.TempDir(), func(string, json.RawMessage) {},
		func(string, json.RawMessage) (any, error) { return nil, errors.New("not supported") })
	if err != nil {
		return err
	}
	defer func() { _ = proc.Close() }() // the answers are all we need from it
	return ask(ctx, rpc)
}

// codexAccount names the account Codex is signed in with, and how much of
// its usage limits it has used. binary is named in the sign-in hint.
func codexAccount(ctx context.Context, rpc *rpcConn, binary string) (string, []Limit, error) {
	type accountResult struct {
		RequiresOpenaiAuth bool `json:"requiresOpenaiAuth"`
		Account            *struct {
			Type     string `json:"type"`     // "chatgpt", "apiKey", "amazonBedrock", "gcp", ...
			PlanType string `json:"planType"` // "free", "plus", "pro", ...
		} `json:"account"`
	}
	a, err := rpc.Call[accountResult](ctx, "account/read", map[string]any{})
	switch {
	case err != nil:
		return "", nil, err
	case a.Account == nil && a.RequiresOpenaiAuth:
		return "", nil, fmt.Errorf("not signed in to Codex: run `%s login` in a terminal, then check again", binary)
	case a.Account == nil:
		return "", nil, nil // a provider that needs no sign-in
	case a.Account.Type != "chatgpt":
		return codexAccountType(a.Account.Type), nil, nil
	}
	name := strings.TrimSpace("ChatGPT " + upperFirst(a.Account.PlanType))

	type window struct {
		UsedPercent        float64 `json:"usedPercent"`
		WindowDurationMins int     `json:"windowDurationMins"`
		ResetsAt           int64   `json:"resetsAt"` // Unix seconds
	}
	type limitsResult struct {
		RateLimits struct {
			Primary   *window `json:"primary"`
			Secondary *window `json:"secondary"`
		} `json:"rateLimits"`
	}
	// The limits are extra: an older Codex without the call still checks out.
	var limits []Limit
	if l, err := rpc.Call[limitsResult](ctx, "account/rateLimits/read", map[string]any{}); err == nil {
		for _, w := range []*window{l.RateLimits.Primary, l.RateLimits.Secondary} {
			if w == nil {
				continue
			}
			limit := Limit{UsedPercent: w.UsedPercent, WindowMinutes: w.WindowDurationMins}
			if w.ResetsAt > 0 { // null or missing when Codex doesn't say
				limit.ResetsAt = time.Unix(w.ResetsAt, 0).UTC().Format(time.RFC3339)
			}
			limits = append(limits, limit)
		}
	}
	return name, limits, nil
}

// codexAccountType names an account that isn't ChatGPT's, so has no plan
// or usage limits.
func codexAccountType(t string) string {
	switch t {
	case "apiKey":
		return "API key"
	case "amazonBedrock":
		return "Amazon Bedrock"
	case "gcp":
		return "GCP"
	}
	return t
}

// codexModelList pages through the models the app-server offers, the
// default first.
func codexModelList(ctx context.Context, rpc *rpcConn) ([]Model, error) {
	out := []Model{}
	cursor := ""
	for range 10 { // pages; a handful of models fits in one
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		type modelPage struct {
			Data []struct {
				Model                     string `json:"model"`
				DisplayName               string `json:"displayName"`
				Description               string `json:"description"`
				Hidden                    bool   `json:"hidden"`
				IsDefault                 bool   `json:"isDefault"`
				DefaultReasoningEffort    string `json:"defaultReasoningEffort"`
				SupportedReasoningEfforts []struct {
					ReasoningEffort string `json:"reasoningEffort"`
					Description     string `json:"description"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		res, err := rpc.Call[modelPage](ctx, "model/list", params)
		if err != nil {
			return nil, err
		}
		for _, m := range res.Data {
			if m.Hidden {
				continue
			}
			model := Model{
				ID:            m.Model,
				Name:          cmp.Or(m.DisplayName, m.Model),
				Description:   m.Description,
				Efforts:       []Effort{},
				DefaultEffort: m.DefaultReasoningEffort,
			}
			for _, e := range m.SupportedReasoningEfforts {
				model.Efforts = append(model.Efforts, Effort{ID: e.ReasoningEffort, Description: e.Description})
			}
			if m.IsDefault {
				out = append([]Model{model}, out...)
			} else {
				out = append(out, model)
			}
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
}
