// Package agent drives the learner's own, already signed-in coding agent CLI
// (Claude Code or Codex) and turns its output into a small set of events,
// while holding the tutor to Tutortop's tool policy.
//
// This file is the contract every adapter meets: [Start], [Session], [Event]
// and [Tool], and [Provider] for the list of agents. policy.go says what the
// tutor may do and how an adapter enforces it. Each CLI's adapter is one file
// (claude.go, codex.go) built from the shared pieces in process.go (the CLI
// process, newline-delimited JSON, decode) and rpc.go (JSON-RPC). The fake CLI
// in fakecli_test.go tests an adapter without the real CLI.
//
// docs/agents.md explains how to add an agent.
package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// Start starts a session with a provider's CLI. It returns once the CLI is
// ready for a prompt and the session has its ID, or an error that explains
// what to fix: the CLI isn't installed, isn't signed in, or didn't start
// (wrapping [ErrStopped] with the end of its stderr if it exited). Setup has
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
	return p.start(ctx, opts, onEvent)
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
	Model    string // a Model.ID; empty uses the CLI's default
	Effort   string // a reasoning Effort.ID, e.g. "high"; empty uses the model's default
}

// Session is one running conversation with an agent CLI.
type Session interface {
	// ID is the provider's session or thread id, never empty. The app saves
	// it and passes it back as Options.ResumeID.
	ID() string
	// Prompt sends a user message and blocks until the agent's turn ends.
	// The app sends one prompt at a time. Every event of the turn has been
	// delivered by the time it returns. It returns:
	//   - nil when the turn ended, including when Cancel ended it;
	//   - an error if the agent reports the turn failed; the session can
	//     take the next prompt;
	//   - an error wrapping ErrStopped if the process exited; the session
	//     is finished;
	//   - ctx's error if ctx ends first, leaving the turn running.
	Prompt(ctx context.Context, text string) error
	// Cancel asks the agent to end the current turn, which Prompt then
	// reports as ending normally. It doesn't block and may be called from
	// any goroutine; without a turn it does nothing.
	Cancel()
	// Close stops the process, giving the CLI a moment to save its session.
	// It doesn't wait for the exit, since callers may hold a lock. It may be
	// called more than once.
	Close() error
}

// ErrStopped means the agent process exited; the session can't be used again.
var ErrStopped = errors.New("the agent stopped")

// Event is something the agent did that the learner may see.
//
// Events reach onEvent from one goroutine, in the order the CLI reported
// them, and the events of a turn arrive before its Prompt returns. A warning
// may also come between turns.
type Event struct {
	Kind EventKind
	Text string // EventText: the next piece of the reply; EventWarning: the message
	Tool Tool   // EventTool
}

// EventKind says what an [Event] carries.
type EventKind string

const (
	// EventText is the next piece of the agent's reply to the learner, not
	// its reasoning. The pieces of a reply, joined, are the reply; a tool
	// event between two pieces splits them into two messages.
	EventText EventKind = "text"
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
// [Providers].
type Provider struct {
	ID     string // saved in settings and workspaces: "claude" | "codex"
	Name   string // shown to people
	Binary string // looked up on PATH; the app reports it missing
	start  func(ctx context.Context, opts Options, onEvent func(Event)) (Session, error)
	models func(ctx context.Context) ([]Model, error)
}

// Providers lists the agents Tutortop can drive, in the order the app offers
// them.
func Providers() []Provider {
	return []Provider{
		{ID: "claude", Name: "Claude Code", Binary: "claude", start: startClaude, models: claudeModels},
		{ID: "codex", Name: "Codex", Binary: "codex", start: startCodex, models: codexModels},
	}
}

// Lookup returns the provider with this id.
func Lookup(id string) (Provider, bool) {
	ps := Providers()
	i := slices.IndexFunc(ps, func(p Provider) bool { return p.ID == id })
	if i < 0 {
		return Provider{}, false
	}
	return ps[i], true
}

// Models lists the models the provider's CLI can use, the default first.
// The app caches the answer for a few minutes, so asking the CLI is fine.
func (p Provider) Models(ctx context.Context) ([]Model, error) { return p.models(ctx) }

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
