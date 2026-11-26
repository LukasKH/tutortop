// Package agent drives the learner's own, already signed-in coding agent CLI
// (Claude Code, Codex or Antigravity) and turns its output into a small set
// of events, while holding the tutor to Tutortop's tool policy.
//
// This file is the contract every adapter meets: [Start], [Session], [Event]
// and [Tool], and [Provider] for the list of agents. policy.go says what the
// tutor may do and how an adapter enforces it. Each CLI's adapter is one file
// (claude.go, codex.go, antigravity.go) built from the shared pieces in
// process.go (the CLI process, newline-delimited JSON) and rpc.go (JSON-RPC);
// antigravity.go speaks the Agent Client Protocol through its SDK. The fake
// CLI in fakecli_test.go tests an adapter without the real CLI.
//
// docs/agents.md explains how to add an agent.
package agent

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"slices"
)

// Start starts a session with a provider's CLI. It returns once the CLI is
// ready for a prompt and the session has its ID, or an error that explains
// what to fix: the CLI isn't installed, isn't signed in, or didn't start
// (wrapping [ErrExited] with the end of its stderr if it exited). Setup has
// its own deadline, so a hung CLI can't block forever. ctx bounds the whole
// session: when it ends, the process is stopped.
//
// onEvent receives the session's events, one at a time and in order (see
// [Event]); it must not call back into the session.
func Start(ctx context.Context, providerID string, opts Options, onEvent func(Event)) (Session, error) {
	p, ok := Lookup(providerID)
	if !ok {
		return nil, fmt.Errorf("unknown agent provider %q", providerID)
	}
	opts.Binary = cmp.Or(opts.Binary, p.Binary)
	switch p.ID {
	case Claude:
		return startClaude(ctx, opts, onEvent)
	case Codex:
		return startCodex(ctx, opts, onEvent)
	case Antigravity:
		return startAntigravity(ctx, opts, onEvent)
	}
	return nil, fmt.Errorf("agent provider %q has no adapter", p.ID)
}

// Options says how to start a session. Every adapter must honour all of them.
type Options struct {
	// Cwd is the learner's workspace: the CLI runs there and the tool policy
	// is relative to it.
	Cwd string
	// MCPName and MCPURL name Tutortop's MCP server, a streamable HTTP
	// endpoint. It must be the only MCP server the agent uses, under this
	// name, and its tool calls must be allowed at least 10 minutes:
	// create_task runs sandbox checks.
	MCPName string
	MCPURL  string
	// Instructions are added to the agent's system prompt, keeping the CLI's
	// own.
	Instructions string
	// ResumeID is a Session.ID from an earlier session to continue. If the
	// CLI can't resume it (deleted, made on another machine), start a fresh
	// session instead of failing; the app saves the new ID.
	ResumeID string
	// Binary is the CLI to run, a path or a name on PATH; empty uses
	// Provider.Binary.
	Binary string
	Model  string // a Model.ID; empty uses the CLI's default
	Effort string // a reasoning Effort.ID, e.g. "high"; empty uses the model's default
	// Subagents lets the agent hand work such as research to subagents.
	// Nothing may outlive the turn, so the CLI must wait for each subagent
	// before the turn goes on; an adapter whose CLI can't promise that, or
	// has no subagents, leaves them off.
	Subagents bool
}

// Session is one running conversation with an agent CLI.
type Session interface {
	// ID is the provider's session or thread id, never empty. The app saves
	// it and passes it back as Options.ResumeID.
	ID() string
	// Prompt sends a user message, with any images attached to it, and
	// blocks until the agent's turn ends. The app sends one prompt at a
	// time. Every event of the turn has been delivered by the time it
	// returns. It returns:
	//   - nil when the turn ended, including when Cancel ended it;
	//   - an error if the agent reports the turn failed, or if it can't
	//     take images and some are attached; the session can take the next
	//     prompt;
	//   - an error wrapping ErrExited if the process exited; the session
	//     is finished;
	//   - ctx's error if ctx ends first, leaving the turn running.
	Prompt(ctx context.Context, text string, images []Image) error
	// Cancel asks the agent to end the current turn, which Prompt then
	// reports as ending normally. It doesn't block and may be called from
	// any goroutine; without a turn it does nothing.
	Cancel()
	// Close stops the process, giving the CLI a moment to save its session.
	// It doesn't wait for the exit, since callers may hold a lock. It may be
	// called more than once.
	Close() error
}

// Image is a picture the learner attached to a prompt, such as a screenshot
// of an error. It's kept in a file, which some CLIs read themselves.
type Image struct {
	Path     string // absolute
	MIMEType string // image/png, image/jpeg, image/gif or image/webp
}

// encoded reads the image, base64-encoded, for CLIs that take its data
// inline.
func (img Image) encoded() (string, error) {
	b, err := os.ReadFile(img.Path)
	if err != nil {
		return "", fmt.Errorf("reading the attached image: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// ErrExited means the agent process exited; the session can't be used again.
// (Stopping a turn is Session.Cancel, which doesn't end the process.)
var ErrExited = errors.New("the agent stopped")

// Event is something the agent did that the learner may see.
//
// Events reach onEvent from one goroutine, in the order the CLI reported
// them, and the events of a turn arrive before its Prompt returns. A warning
// may also come between turns.
type Event struct {
	Kind EventKind
	Text string // EventText, EventThinking: the next piece; EventWarning: the message
	Tool Tool   // EventTool, EventPreparing
}

// EventKind says what an [Event] carries.
type EventKind string

const (
	// EventText is the next piece of the agent's reply to the learner, not
	// its reasoning. The pieces of a reply, joined, are the reply; a tool
	// event between two pieces splits them into two messages.
	EventText EventKind = "text"
	// EventThinking is the next piece of the agent's reasoning, as far as
	// the CLI shows it (often a summary). It's progress to show while the
	// agent works, not part of the reply, and isn't kept. Pieces of one
	// stretch of reasoning, joined, are its text.
	EventThinking EventKind = "thinking"
	// EventPreparing is the agent starting to write a tool call's input,
	// which can take a while for a long one such as a plan or a task. Tool
	// has its ID and what its name tells (Kind, MCPServer, MCPTool, Subagent)
	// but no Status, Title or Paths; an EventTool with the same ID reports
	// the call once it's written. Only adapters whose CLI streams a call as
	// it's written send it.
	EventPreparing EventKind = "preparing"
	// EventTool is a tool call starting, or changing status (see [Tool]).
	EventTool EventKind = "tool"
	// EventWarning is something the learner should know, e.g. Tutortop's
	// MCP server didn't connect. Report that if the CLI tells you.
	EventWarning EventKind = "warning"
)

// Tool is one tool call. Its first event describes it in full; later events
// for the same ID may carry only ID and Status. Every call should end with
// ToolDone or ToolFailed; the app counts one still running when the turn ends
// as done. IDs are unique within a session.
type Tool struct {
	ID        string
	Status    ToolStatus
	Kind      ToolKind
	Title     string   // what it does, for people: "Ran go test", "Searched the web: goroutines"
	Paths     []string // files it reads or changes, as the CLI gave them
	MCPServer string   // for MCP tools, the server and tool name
	MCPTool   string
	Subagent  bool // the call starts a subagent; allowed only with Options.Subagents
}

// ToolStatus is where a tool call is. A declined call is ToolFailed.
type ToolStatus string

const (
	ToolRunning ToolStatus = "running"
	ToolDone    ToolStatus = "done"
	ToolFailed  ToolStatus = "failed"
)

// ToolKind says what a tool call does. The app shows calls by kind, and the
// policy decides by kind, so a call that can change files must be ToolEdit
// with its Paths. The kinds are the Agent Client Protocol's, so an agent that
// speaks ACP maps onto them directly.
type ToolKind string

const (
	ToolRead    ToolKind = "read"
	ToolEdit    ToolKind = "edit"
	ToolSearch  ToolKind = "search"  // searching files
	ToolExecute ToolKind = "execute" // shell commands
	ToolFetch   ToolKind = "fetch"   // web search and fetch
	ToolThink   ToolKind = "think"   // the agent's own bookkeeping, e.g. its todo list
	ToolOther   ToolKind = "other"   // MCP tools and anything else
)

// Provider is one coding agent CLI. Adding an agent means adding it to
// [Providers], and its adapter to the switches on [ProviderID].
type Provider struct {
	ID     ProviderID
	Name   string // shown to people
	Binary string // looked up on PATH; the app reports it missing
}

// ProviderID names an agent in settings and workspaces.
type ProviderID string

const (
	Claude      ProviderID = "claude"
	Codex       ProviderID = "codex"
	Antigravity ProviderID = "antigravity"
)

// Providers lists the agents Tutortop can drive, in the order the app offers
// them.
func Providers() []Provider {
	return []Provider{
		{ID: Claude, Name: "Claude Code", Binary: "claude"},
		{ID: Codex, Name: "Codex", Binary: "codex"},
		{ID: Antigravity, Name: "Antigravity", Binary: "agy"},
	}
}

// Lookup returns the provider with this id.
func Lookup(id string) (Provider, bool) {
	ps := Providers()
	i := slices.IndexFunc(ps, func(p Provider) bool { return p.ID == ProviderID(id) })
	if i < 0 {
		return Provider{}, false
	}
	return ps[i], true
}

// Models lists the models the provider's CLI can use, the default first.
// binary is the CLI to ask, as in Options.Binary. The app caches the answer
// for a few minutes, so asking the CLI is fine.
func (p Provider) Models(ctx context.Context, binary string) ([]Model, error) {
	binary = cmp.Or(binary, p.Binary)
	switch p.ID {
	case Claude:
		return claudeModels(ctx)
	case Codex:
		return codexModels(ctx, binary)
	case Antigravity:
		return antigravityModels(ctx, binary)
	}
	return nil, fmt.Errorf("agent provider %q has no adapter", p.ID)
}

// Model is one model a provider offers, with the reasoning efforts it accepts.
type Model struct {
	ID          string   `json:"id"`   // passed to the CLI; "" means the CLI's own default
	Name        string   `json:"name"` // shown to people
	Description string   `json:"description,omitempty"`
	Efforts     []Effort `json:"efforts"`
	// DefaultEffort is used when none is chosen; "" means the CLI decides.
	DefaultEffort string `json:"defaultEffort,omitempty"`
}

// Effort is one reasoning level.
type Effort struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

// optional returns prefix+s, or "" if s is empty: "Searched files" + optional(": ", pattern).
func optional(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}
