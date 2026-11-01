// Package agent drives the learner's own, already signed-in coding agent CLI
// (Claude Code or Codex) and turns its output into a small set of events.
package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// Provider is one coding agent CLI. Adding an agent means adding it here.
type Provider struct {
	ID     string // "claude" | "codex"
	Name   string
	Binary string
	start  func(ctx context.Context, opts Options, onEvent func(Event)) (Session, error)
	models func(ctx context.Context) ([]Model, error)
}

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

// Models lists the models the provider's CLI can use.
func (p Provider) Models(ctx context.Context) ([]Model, error) { return p.models(ctx) }

type Options struct {
	Cwd          string
	MCPName      string
	MCPURL       string // streamable HTTP MCP endpoint the agent should use
	Instructions string // appended to the agent's system prompt
	ResumeID     string // provider session/thread id to resume, if any
	Model        string // model id or alias; empty uses the CLI's default
	Effort       string // reasoning effort, e.g. "high"; empty uses the model's default
}

// Event kinds.
const (
	EventText    = "text"    // assistant text delta
	EventTool    = "tool"    // tool call started or finished
	EventWarning = "warning" // something the learner should know, e.g. our tools didn't connect
)

type Event struct {
	Kind string
	Text string // EventText: the next piece of the reply; EventWarning: the message
	Tool Tool   // EventTool
}

// ToolKind says what a tool call does. The kinds are the Agent Client
// Protocol's, so an agent that speaks ACP maps onto them directly.
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

// Tool statuses.
const (
	ToolRunning = "running"
	ToolDone    = "done"
	ToolFailed  = "failed"
)

// Tool is a tool call. Events after the first may carry only ID and Status.
type Tool struct {
	ID        string
	Status    string
	Kind      ToolKind
	Title     string   // what it does, for people: "Ran go test", "Searched the web: goroutines"
	Paths     []string // files it reads or changes
	MCPServer string   // for MCP tools, the server and tool name
	MCPTool   string
}

type Session interface {
	// ID is the provider's session or thread id; save it to resume later.
	ID() string
	// Prompt sends a user message and blocks until the agent's turn ends.
	// If the agent process exits, the error wraps ErrStopped.
	Prompt(ctx context.Context, text string) error
	// Cancel interrupts the current turn.
	Cancel()
	Close() error
}

// ErrStopped means the agent process exited; the session can't be used again.
var ErrStopped = errors.New("the agent stopped")

func Start(ctx context.Context, providerID string, opts Options, onEvent func(Event)) (Session, error) {
	p, ok := Lookup(providerID)
	if !ok {
		return nil, fmt.Errorf("unknown agent provider %q", providerID)
	}
	return p.start(ctx, opts, onEvent)
}

func optional(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}
