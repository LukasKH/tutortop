package agent

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/adrg/xdg"
	"github.com/coder/acp-go-sdk"

	"github.com/LukasKH/tutortop/internal/decode"
)

// antigravitySession drives the learner's `agy` over the Agent Client
// Protocol (`agy acp`). agy keeps Tutortop's settings and conversations in a
// profile folder of its own, apart from the learner's ~/.gemini, because
// Tutortop configures it (see setUpAntigravity). The sign-in
// lives in the system keyring, so a learner signed in to agy is signed in
// here too.
type antigravitySession struct {
	*process

	conn   *acp.ClientSideConnection
	client *antigravityClient
	id     acp.SessionId
}

func startAntigravity(ctx context.Context, opts Options, onEvent func(Event)) (Session, error) {
	if err := setUpAntigravity(); err != nil {
		return nil, fmt.Errorf("setting up Antigravity: %w", err)
	}
	client := &antigravityClient{policy: newPolicy(opts), mcpName: opts.MCPName, onEvent: onEvent}
	proc, conn, err := startAntigravityACP(ctx, opts.Binary, opts.Cwd, client)
	if err != nil {
		return nil, err
	}
	s := &antigravitySession{process: proc, conn: conn, client: client}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := s.open(ctx, opts); err != nil {
		return nil, antigravityAbort(proc, conn, err)
	}
	return s, nil
}

func (s *antigravitySession) ID() string { return string(s.id) }

func (s *antigravitySession) Prompt(ctx context.Context, text string) error {
	s.client.startTurn()
	defer s.client.endTurn()
	res, err := s.conn.Prompt(ctx, acp.PromptRequest{SessionId: s.id, Prompt: []acp.ContentBlock{acp.TextBlock(text)}})
	if err != nil {
		return s.failed(ctx, err)
	}
	switch res.StopReason {
	case acp.StopReasonEndTurn, acp.StopReasonCancelled:
		return nil
	case acp.StopReasonMaxTokens, acp.StopReasonMaxTurnRequests, acp.StopReasonRefusal:
	}
	return fmt.Errorf("turn failed in Antigravity: it stopped with %s", strings.ReplaceAll(string(res.StopReason), "_", " "))
}

// Cancel sends session/cancel; agy then ends the turn as cancelled.
func (s *antigravitySession) Cancel() {
	if !s.client.cancel() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// If agy is gone, there's no turn to cancel, and Prompt reports the exit.
		_ = s.conn.Cancel(ctx, acp.CancelNotification{SessionId: s.id})
	}()
}

// open resumes opts.ResumeID, or starts a new session, and chooses its model
// and thinking level.
func (s *antigravitySession) open(ctx context.Context, opts Options) error {
	var meta acp.Meta
	// agy advertises this in initialize (_meta.antigravity.systemPrompt.append):
	// the text goes after agy's own system prompt.
	if err := meta.Set("antigravity", map[string]any{"systemPrompt": map[string]any{"append": opts.Instructions}}); err != nil {
		return err
	}
	servers := []acp.McpServer{{Http: &acp.McpServerHttpInline{
		Type:    "http",
		Name:    opts.MCPName,
		Url:     opts.MCPURL,
		Headers: []acp.HttpHeader{},
	}}}

	var options []acp.SessionConfigOption
	if opts.ResumeID != "" {
		// session/resume, unlike session/load, doesn't replay the old turns.
		res, err := s.conn.ResumeSession(ctx, acp.ResumeSessionRequest{
			Meta:       meta,
			SessionId:  acp.SessionId(opts.ResumeID),
			Cwd:        opts.Cwd,
			McpServers: servers,
		})
		if err == nil {
			s.id, options = acp.SessionId(opts.ResumeID), res.ConfigOptions
		}
	}
	if s.id == "" {
		res, err := s.conn.NewSession(ctx, acp.NewSessionRequest{Meta: meta, Cwd: opts.Cwd, McpServers: servers})
		if err != nil {
			return antigravitySetupError(opts.Binary, err)
		}
		s.id, options = res.SessionId, res.ConfigOptions
	}

	for _, choice := range []struct {
		category acp.SessionConfigOptionCategory
		value    string
	}{{acp.SessionConfigOptionCategoryModel, opts.Model}, {acp.SessionConfigOptionCategoryThoughtLevel, opts.Effort}} {
		option := antigravityOption(options, choice.category)
		if choice.value == "" || option == nil {
			continue
		}
		_, err := s.conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: s.id,
			ConfigId:  option.Id,
			Value:     acp.SessionConfigValueId(choice.value),
		}})
		if err != nil {
			return fmt.Errorf("choosing the %s %q in Antigravity: %s", strings.ToLower(option.Name), choice.value, antigravityReason(err))
		}
	}
	return nil
}

// failed explains a prompt agy didn't answer: ctx ended, agy is gone, or
// agy reported an error.
func (s *antigravitySession) failed(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if antigravityGone(s.process, s.conn) {
		// Without its connection the session can't go on, even if agy
		// still runs (its output overflowed the SDK, say).
		_ = s.Close()
		<-s.done
		return s.exitError()
	}
	return fmt.Errorf("turn failed in Antigravity: %s", antigravityReason(err))
}

// antigravityClient is Tutortop's side of ACP. It turns agy's session updates
// into events and answers its permission requests with the policy. It offers
// no terminal and no file access, so those methods refuse.
type antigravityClient struct {
	policy  policy
	mcpName string
	onEvent func(Event)

	mu        sync.Mutex
	prompting bool                          // a Prompt is running
	cancelled bool                          // Cancel ended the running turn
	tools     map[acp.ToolCallId]ToolStatus // unfinished calls, by the status last reported
}

func (c *antigravityClient) SessionUpdate(_ context.Context, n acp.SessionNotification) error {
	u := n.Update
	switch {
	case u.AgentMessageChunk != nil:
		if t := u.AgentMessageChunk.Content.Text; t != nil {
			c.onEvent(Event{Kind: EventText, Text: t.Text})
		}
	case u.AgentThoughtChunk != nil:
		if t := u.AgentThoughtChunk.Content.Text; t != nil {
			c.onEvent(Event{Kind: EventThinking, Text: t.Text})
		}
	case u.ToolCall != nil:
		call := u.ToolCall
		t := c.describe(call.ToolCallId, ptrValue(call.Name), call.Title, call.Kind, call.Locations)
		t.Status = antigravityStatus(call.Status)
		c.report(t)
	case u.ToolCallUpdate != nil:
		status, ok := u.ToolCallUpdate.Status.Value()
		if ok {
			c.report(Tool{ID: string(u.ToolCallUpdate.ToolCallId), Status: antigravityStatus(status)})
		}
	}
	return nil
}

// RequestPermission decides a tool call with the policy, and picks the
// matching allow-once or reject-once option.
func (c *antigravityClient) RequestPermission(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	if c.turnCancelled() {
		// ACP's rule: a cancelled turn's pending requests get this outcome.
		return acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}}}, nil
	}
	call := req.ToolCall
	name, _ := call.Name.Value()
	title, _ := call.Title.Value()
	kind, _ := call.Kind.Value()
	locations, _ := call.Locations.Value()
	want := acp.PermissionOptionKindRejectOnce
	if c.policy.allow(c.describe(call.ToolCallId, name, title, kind, locations)) == nil {
		want = acp.PermissionOptionKindAllowOnce
	}
	for _, o := range req.Options {
		if o.Kind == want {
			return acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{OptionId: o.OptionId}}}, nil
		}
	}
	return acp.RequestPermissionResponse{}, fmt.Errorf("no %s option in the permission request", want)
}

func (c *antigravityClient) ReadTextFile(context.Context, acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, acp.NewMethodNotFound(acp.ClientMethodFsReadTextFile)
}

func (c *antigravityClient) WriteTextFile(context.Context, acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, acp.NewMethodNotFound(acp.ClientMethodFsWriteTextFile)
}

func (c *antigravityClient) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalCreate)
}

func (c *antigravityClient) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalKill)
}

func (c *antigravityClient) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalOutput)
}

func (c *antigravityClient) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalRelease)
}

func (c *antigravityClient) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalWaitForExit)
}

func (c *antigravityClient) CreateElicitation(context.Context, acp.CreateElicitationRequest) (acp.CreateElicitationResponse, error) {
	return acp.CreateElicitationResponse{}, acp.NewMethodNotFound(acp.ClientMethodElicitationCreate)
}

func (c *antigravityClient) CompleteElicitation(context.Context, acp.CompleteElicitationNotification) error {
	return nil // we never start one
}

func (c *antigravityClient) startTurn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prompting, c.cancelled = true, false
}

func (c *antigravityClient) endTurn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prompting = false
}

// cancel marks the running turn cancelled, and reports whether there is one.
func (c *antigravityClient) cancel() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelled = c.prompting
	return c.prompting
}

func (c *antigravityClient) turnCancelled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cancelled
}

// report sends a tool event, unless it repeats the call's last status: agy
// reports in_progress, then pending while it asks, then in_progress again.
func (c *antigravityClient) report(t Tool) {
	c.mu.Lock()
	last, known := c.tools[acp.ToolCallId(t.ID)]
	switch {
	case t.Kind == "" && (!known || last == t.Status):
		c.mu.Unlock()
		return // a call we never saw start, or no change
	case t.Status == ToolRunning:
		if c.tools == nil {
			c.tools = map[acp.ToolCallId]ToolStatus{}
		}
		c.tools[acp.ToolCallId(t.ID)] = t.Status
	default:
		delete(c.tools, acp.ToolCallId(t.ID))
	}
	c.mu.Unlock()
	c.onEvent(Event{Kind: EventTool, Tool: t})
}

// describe turns what agy says about a tool call into a Tool. agy names an
// MCP tool mcp_<server>_<tool>, so our server's tools start with
// mcp_<MCPName>_. Server names may hold underscores, so another server's
// tool can't be split reliably; it becomes a tool of kind other, which the
// policy refuses.
func (c *antigravityClient) describe(id acp.ToolCallId, name, title string, kind acp.ToolKind, locations []acp.ToolCallLocation) Tool {
	t := Tool{ID: string(id), Title: title, Kind: antigravityKind(kind)}
	for _, l := range locations {
		if l.Path != "" { // an empty path would pass as the workspace itself
			t.Paths = append(t.Paths, l.Path)
		}
	}
	if !strings.HasPrefix(name, "mcp_") {
		return t
	}
	// Whatever kind agy gives another server's tool, the policy refuses it.
	t.Kind = ToolOther
	if tool, ok := strings.CutPrefix(name, "mcp_"+c.mcpName+"_"); ok && tool != "" {
		t.MCPServer, t.MCPTool = c.mcpName, tool
	}
	return t
}

// antigravityModels lists the models and thinking levels a short-lived `agy
// acp` session offers, in the temporary folder: ACP reports them per session.
func antigravityModels(ctx context.Context, binary string) ([]Model, error) {
	models, _, err := askAntigravity(ctx, binary)
	return models, err
}

// askAntigravity opens a short-lived `agy acp` session for the models it
// offers and the account's plan, such as "Google AI Pro" ("" if agy doesn't
// say). Opening it fails unless agy is signed in.
func askAntigravity(ctx context.Context, binary string) ([]Model, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dir := os.TempDir()
	proc, conn, err := startAntigravityACP(ctx, binary, dir, &antigravityClient{onEvent: func(Event) {}})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = proc.Close() }() // the answers are all we need from it
	res, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: dir, McpServers: []acp.McpServer{}})
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		var re *acp.RequestError
		if errors.As(err, &re) {
			return nil, "", antigravitySetupError(binary, err)
		}
		return nil, "", antigravityAbort(proc, conn, antigravitySetupError(binary, err))
	}
	meta, _ := decode.JSON[struct { // the plan is extra; without it the check still passes
		Tier struct {
			Name string `json:"name"`
		} `json:"tier"`
	}](res.Meta["antigravity"])
	return antigravitySessionModels(res), meta.Tier.Name, nil
}

// antigravitySessionModels reads the models and thinking levels from a new
// session's options, the current model first.
func antigravitySessionModels(res acp.NewSessionResponse) []Model {
	efforts := []Effort{}
	if o := antigravityOption(res.ConfigOptions, acp.SessionConfigOptionCategoryThoughtLevel); o != nil {
		for _, v := range antigravityValues(o) {
			efforts = append(efforts, Effort{ID: string(v.Value), Description: v.Name})
		}
	}
	o := antigravityOption(res.ConfigOptions, acp.SessionConfigOptionCategoryModel)
	if o == nil || len(antigravityValues(o)) == 0 {
		return []Model{{Name: "Antigravity's default", Efforts: []Effort{}}} // the CLI chooses
	}
	models := []Model{}
	for _, v := range antigravityValues(o) {
		m := Model{ID: string(v.Value), Name: cmp.Or(v.Name, string(v.Value)), Description: ptrValue(v.Description), Efforts: efforts}
		if v.Value == o.CurrentValue {
			models = append([]Model{m}, models...)
		} else {
			models = append(models, m)
		}
	}
	return models
}

// startAntigravityACP starts `agy acp` on Tutortop's profile and completes
// the handshake. We offer agy no terminal and no file access, so it uses its
// own tools, which ask permission (see setUpAntigravity).
func startAntigravityACP(ctx context.Context, binary, cwd string, client acp.Client) (*process, *acp.ClientSideConnection, error) {
	proc, err := startProcess(ctx, cwd, binary, nil, "acp", "--gemini-dir", antigravityProfile())
	if err != nil {
		return nil, nil, err
	}
	conn := acp.NewClientSideConnection(client, proc.stdin, proc.stdout)
	initCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	_, err = conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientInfo:      &acp.Implementation{Name: "tutortop", Title: acp.Ptr("Tutortop"), Version: "0.2.7"}, // x-release-please-version
	})
	if err != nil {
		return nil, nil, antigravityAbort(proc, conn, fmt.Errorf("starting Antigravity: %w", err))
	}
	return proc, conn, nil
}

// antigravityProfile is agy's folder for Tutortop (its --gemini-dir).
func antigravityProfile() string {
	return filepath.Join(xdg.DataHome, "tutortop", "antigravity")
}

// setUpAntigravity configures Tutortop's profile so agy asks before every
// tool call it can, and refuses the ones it can't ask about. Over ACP, agy's
// default mode writes files anywhere without asking and ignores
// permissions.deny rules, so:
//   - toolPermission "strict" in its settings makes it ask before every read,
//     write, command and MCP call, which RequestPermission puts through the
//     policy. The settings file also holds agy's own, such as the sign-in
//     method, so the rest of it is kept.
//   - a PreToolUse hook denies the tools that run without asking even then:
//     subagents, messages between them, and scheduled or background work,
//     which could outlive the turn. Commands do ask; the hook refuses them
//     too.
func setUpAntigravity() error {
	antigravitySetUpMu.Lock()
	defer antigravitySetUpMu.Unlock()
	profile := antigravityProfile()
	settingsPath := filepath.Join(profile, "antigravity-cli", "settings.json")
	settings := map[string]json.RawMessage{}
	if b, err := os.ReadFile(settingsPath); err == nil {
		if settings, err = decode.JSON[map[string]json.RawMessage](b); err != nil {
			return fmt.Errorf("reading %s: %w", settingsPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if settings == nil { // the file holds null
		settings = map[string]json.RawMessage{}
	}
	settings["toolPermission"] = json.RawMessage(`"strict"`)
	deny := `{"decision":"deny","reason":"not available in Tutortop"}`
	hooks := map[string]any{"tutortop": map[string]any{"PreToolUse": []any{map[string]any{
		"matcher": "^(" + strings.Join(antigravityDenied, "|") + ")$",
		"hooks":   []any{map[string]any{"type": "command", "command": echoCommand(deny)}},
	}}}}
	if err := writeJSON(settingsPath, settings); err != nil {
		return err
	}
	return writeJSON(filepath.Join(profile, "config", "hooks.json"), hooks)
}

// antigravitySetUpMu keeps sessions that start together from writing the
// profile at once.
var antigravitySetUpMu sync.Mutex

// antigravityDenied are the tools the hook refuses.
var antigravityDenied = []string{
	"run_command",
	"invoke_subagent", "define_subagent", "manage_subagents", "send_message",
	"schedule", "manage_task", "run_workflow", "swarm",
}

// antigravitySetupError explains why agy wouldn't open a session.
func antigravitySetupError(binary string, err error) error {
	var re *acp.RequestError
	if errors.As(err, &re) && re.Code == antigravityAuthRequired {
		// Quoted for the shell, not with %q, which would double a Windows path's backslashes.
		return fmt.Errorf("not signed in to Antigravity: run `%s acp --login --gemini-dir \"%s\"` in a terminal, then try again", binary, antigravityProfile())
	}
	return fmt.Errorf("starting Antigravity: %s", antigravityReason(err))
}

// antigravityAuthRequired is ACP's auth_required error code.
const antigravityAuthRequired = -32000

// antigravityReason is the message of an error agy returned, without the
// JSON-RPC wrapping.
func antigravityReason(err error) string {
	var re *acp.RequestError
	if errors.As(err, &re) {
		return re.Message
	}
	return err.Error()
}

// antigravityAbort stops agy after a request failed. If agy is gone, its
// exit explains the failure better than err.
func antigravityAbort(proc *process, conn *acp.ClientSideConnection, err error) error {
	gone := antigravityGone(proc, conn)
	_ = proc.Close()
	if !gone {
		return err
	}
	<-proc.done
	return proc.exitError()
}

// antigravityGone reports whether agy exited or its connection ended. A
// write to an agy that just exited fails before its output ends, so it gives
// the exit a moment to show.
func antigravityGone(proc *process, conn *acp.ClientSideConnection) bool {
	select {
	case <-conn.Done():
		return true
	case <-proc.done:
		return true
	case <-time.After(time.Second):
		return false
	}
}

// antigravityOption returns the select option of this category, or nil.
func antigravityOption(options []acp.SessionConfigOption, category acp.SessionConfigOptionCategory) *acp.SessionConfigOptionSelect {
	for _, o := range options {
		if o.Select != nil && o.Select.Category != nil && *o.Select.Category == category {
			return o.Select
		}
	}
	return nil
}

// antigravityValues lists a select's values, grouped or not.
func antigravityValues(o *acp.SessionConfigOptionSelect) []acp.SessionConfigSelectOption {
	if o.Options.Ungrouped != nil {
		return *o.Options.Ungrouped
	}
	var values []acp.SessionConfigSelectOption
	if o.Options.Grouped != nil {
		for _, g := range *o.Options.Grouped {
			values = append(values, g.Options...)
		}
	}
	return values
}

// antigravityKind maps ACP's tool kinds onto ours. Deleting and moving
// change files, so the policy judges them as edits; switching modes (for
// one, to approve edits without asking) falls to other, which it refuses.
func antigravityKind(k acp.ToolKind) ToolKind {
	switch k {
	case acp.ToolKindRead:
		return ToolRead
	case acp.ToolKindEdit, acp.ToolKindDelete, acp.ToolKindMove:
		return ToolEdit
	case acp.ToolKindSearch:
		return ToolSearch
	case acp.ToolKindExecute:
		return ToolExecute
	case acp.ToolKindThink:
		return ToolThink
	case acp.ToolKindFetch:
		return ToolFetch
	case acp.ToolKindSwitchMode, acp.ToolKindOther:
	}
	return ToolOther
}

func antigravityStatus(s acp.ToolCallStatus) ToolStatus {
	switch s {
	case acp.ToolCallStatusCompleted:
		return ToolDone
	case acp.ToolCallStatusFailed:
		return ToolFailed
	case acp.ToolCallStatusPending, acp.ToolCallStatusInProgress:
	}
	return ToolRunning
}

// writeJSON writes v to path as JSON, unless the file already holds it.
// agy may be reading the file for another session, so it's replaced whole,
// never seen half written.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err = errors.Join(err, f.Close()); err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name()) // the half-written copy
	}
	return err
}

// echoCommand is a shell command that prints s, which must not hold a
// single quote: agy runs hooks with sh -c, where s goes in single quotes, or
// cmd /c on Windows, whose echo prints the rest of the line as it is.
func echoCommand(s string) string {
	if runtime.GOOS == "windows" {
		return "echo " + s
	}
	return "echo '" + s + "'"
}

// ptrValue returns *p, or the zero value for nil.
func ptrValue[T any](p *T) T {
	if p == nil {
		return *new(T)
	}
	return *p
}
