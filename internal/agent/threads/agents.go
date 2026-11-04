// Package threads runs each thread's agent: the chats' tutors and the
// background writers (Agents, Writers). It turns their replies into chat
// transcripts, tells them what to do (prompts/), and serves the chats to the
// frontend (ChatService).
package threads

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/mcp"
	"github.com/LukasKH/tutortop/internal/settings"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Agents runs each thread's agent, at most one per thread. The app sends a
// thread a message (Send), reads what the agents are doing (State) and ends
// a deleted workspace's agents (EndWorkspace); the MCP tools show sandbox
// checks (Show). Inside this package, the writers queue messages, and the
// chats stop a thread's turn and end all agents as the app quits.
//
// A thread's agent (threadAgent):
//
//   - starts with the first message queued for its thread, in the
//     background: queue doesn't wait for the start. A writer's agent opens a
//     fresh conversation; any other resumes the thread's saved one.
//   - keeps its thread busy from the moment a message is queued until it has
//     answered every queued message. That includes opening the CLI, which
//     can take several seconds, so the learner sees the tutor working
//     meanwhile.
//   - posts why in the thread's chat, once, if its CLI doesn't open, drops
//     the waiting messages and ends; the thread turns idle.
//   - restarts with the next message once it's idle, if the settings changed.
//   - ends when its CLI exits, its workspace is deleted or the app quits. A
//     writer also ends as soon as it's idle (its writing is done), and a
//     busy writer takes no second message (it's already writing its
//     objective). Starting any thread but the plan ends the workspace's idle
//     task and lesson agents, so moving through tasks doesn't pile up
//     processes; their conversations resume if they're reopened.
//
// Each verb here means one thing: a thread agent starts and ends, its CLI
// session opens and closes, a message is queued and then answered, and stop
// interrupts the current turn.
type Agents struct {
	settings *settings.Store // the agent, model and effort, read for every message
	mcp      *mcp.Server
	cli      CLI

	mu     sync.Mutex
	agents map[threadKey]*threadAgent
	seq    uint64 // numbers busy and check changes and snapshots in order
}

var _ mcp.Checks = (*Agents)(nil)

// NewAgents runs thread agents on the agent, model and effort in the
// settings, opening their CLI sessions with cli. Each thread's tools are
// served by tools.
func NewAgents(settingsStore *settings.Store, tools *mcp.Server, cli CLI) *Agents {
	return &Agents{settings: settingsStore, mcp: tools, cli: cli, agents: map[threadKey]*threadAgent{}, seq: firstSeq()}
}

// Send posts a message in a thread's chat and queues it for that thread's
// agent, without waiting for the agent to start. Once the message is in the
// chat, whatever goes wrong with it is posted there too.
func (ag *Agents) Send(w *workspace.Workspace, t workspace.Thread, m Message) {
	Post(w, workspace.ChatItem{Thread: t.String(), Role: m.Role, Text: m.Shown})
	if err := ag.queue(w, t, m.Agent); err != nil {
		postFailure(w, t.String(), err.Error())
	}
}

// Message is one thing sent to the tutor. What the learner sees in the chat
// (Shown) can differ from what the tutor receives (Agent): the app attaches
// the open task as context, and its own instructions to the tutor are shown
// as short notices.
type Message struct {
	Role  workspace.ChatRole // workspace.RoleUser for the learner's words, RoleNotice for app events
	Shown string
	Agent string
}

// The UI learns which threads are busy, and how writers' sandbox checks go,
// from both snapshots (State) and events (setPending, Show). All three
// take the next number from ag.seq under ag.mu, so the numbers order them
// exactly as they happened: the frontend keeps whichever is newer, and a
// snapshot read before an event doesn't undo it.

// Show tells the UI how a sandbox check is going (mcp.Checks). A writer's
// is also kept on its threadAgent, so the objective's page shows it even
// after the learner looks away; it goes when the writer does.
func (ag *Agents) Show(wsID string, c events.SandboxCheck) {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	if ta := ag.agents[threadKey{wsID, c.Thread}]; ta != nil && c.Kind == events.CheckTask {
		ta.check = &c
	}
	ag.seq++
	events.Emit(events.CheckStatus, events.Check{Workspace: wsID, Check: c, Seq: ag.seq})
}

// State returns what the workspace's agents are doing: which threads are
// busy, and the writers' latest sandbox checks by objective. Its seq also
// orders snapshots among themselves.
func (ag *Agents) State(wsID string) (busy map[string]bool, checks map[string]events.SandboxCheck, seq uint64) {
	busy, checks = map[string]bool{}, map[string]events.SandboxCheck{}
	ag.mu.Lock()
	defer ag.mu.Unlock()
	ag.seq++
	for _, ta := range ag.agents {
		if ta.key.ws != wsID {
			continue
		}
		if ta.pending > 0 {
			busy[ta.key.name] = true
		}
		if ta.check != nil {
			checks[ta.thread.ID] = *ta.check
		}
	}
	return busy, checks, ag.seq
}

// EndWorkspace ends a workspace's agents and drops their MCP servers,
// before it's deleted.
func (ag *Agents) EndWorkspace(wsID string) {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	for key, ta := range ag.agents {
		if key.ws == wsID {
			ag.end(ta)
		}
	}
	ag.mcp.Forget(wsID)
}

// CLI opens a session with an agent's CLI. InstalledCLI opens the
// learner's own; tests fake it.
type CLI interface {
	Start(ctx context.Context, provider string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error)
}

// InstalledCLI starts the agent CLI installed on the computer.
type InstalledCLI struct{}

func (InstalledCLI) Start(ctx context.Context, provider string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error) {
	return agent.Start(ctx, provider, opts, onEvent)
}

// queue queues a message for a thread's agent, starting the agent if it
// isn't running. It doesn't wait for the start: the CLI opens in the
// background, and if it doesn't, one notice is posted in the thread's chat.
// It fails only if too many messages are already waiting.
func (ag *Agents) queue(w *workspace.Workspace, t workspace.Thread, msg string) error {
	key := threadKey{w.ID, t.String()}
	cfg := configOf(ag.settings.Load())
	ag.mu.Lock()
	defer ag.mu.Unlock()
	ta := ag.agents[key]
	if ta != nil && ta.pending > 0 && t.Kind == workspace.WriterKind {
		return nil // already writing its objective
	}
	// The settings changed since it started: restart it with the new ones.
	// A busy agent finishes its queue first.
	if ta != nil && ta.pending == 0 && ta.cfg != cfg {
		ag.end(ta)
		ta = nil
	}
	if ta == nil {
		ta = ag.start(w, t, cfg)
	}
	// Queued under ag.mu, so an agent that ends can't take a message after
	// it has left the agents.
	select {
	case ta.queue <- msg:
	default:
		return errors.New("too many messages are waiting; let the tutor catch up first")
	}
	ag.setPending(ta, ta.pending+1)
	return nil
}

// stop interrupts the current turn in a thread. Its agent posts a notice
// once the cut-off reply is saved, and takes the next message as usual.
func (ag *Agents) stop(wsID, thread string) {
	ag.mu.Lock()
	ta := ag.agents[threadKey{wsID, thread}]
	ag.mu.Unlock()
	if ta == nil || !ta.opened() {
		return
	}
	ta.stopped.Store(true)
	ta.cli.Cancel()
}

// endAll ends every agent, as the app quits.
func (ag *Agents) endAll() {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	for _, ta := range ag.agents {
		ag.end(ta)
	}
}

// firstSeq is where Agents.seq starts. Counting from the time in
// microseconds keeps the numbers rising across a restart, for a page that
// outlives one in server mode.
func firstSeq() uint64 {
	return uint64(time.Now().UnixMicro())
}

// agentConfig is what a thread's agent is started with. One started with an
// older config restarts once it's idle.
type agentConfig struct {
	Provider string
	Model    string
	Effort   string
}

// configOf is the agent, model and effort the settings choose.
func configOf(s settings.Settings) agentConfig {
	c := s.Models[s.Provider]
	return agentConfig{Provider: s.Provider, Model: c.Model, Effort: c.Effort}
}

// A threadAgent is one thread's running agent: its CLI session and the
// queue of messages it answers, one turn at a time, in order.
type threadAgent struct {
	key     threadKey
	thread  workspace.Thread
	cfg     agentConfig // the agent, model and effort it was started with
	queue   chan string
	cancel  context.CancelFunc
	once    sync.Once   // close runs once
	stopped atomic.Bool // the learner pressed Stop during the current turn

	ready chan struct{} // closed once the CLI has opened, or failed to
	cli   agent.Session // set before ready is closed; nil if it didn't open

	// Guarded by Agents.mu.
	pending int                  // messages queued or being answered; the thread is busy while it's above 0
	check   *events.SandboxCheck // a writer's latest sandbox check of its task
}

// opened reports whether the CLI is open, without blocking.
func (ta *threadAgent) opened() bool {
	select {
	case <-ta.ready:
		return ta.cli != nil
	default:
		return false
	}
}

// close closes the CLI, if it's open or opening.
func (ta *threadAgent) close() {
	ta.once.Do(func() {
		ta.cancel()
		if ta.opened() {
			_ = ta.cli.Close()
		}
	})
}

// threadKey names one thread of one workspace by the thread's name.
type threadKey struct{ ws, name string }

// start starts a thread's agent. Call with ag.mu held.
func (ag *Agents) start(w *workspace.Workspace, t workspace.Thread, cfg agentConfig) *threadAgent {
	ctx, cancel := context.WithCancel(context.Background())
	ta := &threadAgent{key: threadKey{w.ID, t.String()}, thread: t, cfg: cfg, queue: make(chan string, 32), cancel: cancel, ready: make(chan struct{})}
	ag.agents[ta.key] = ta
	go ag.loop(ctx, w, ta)
	if t.Kind != workspace.PlanKind {
		for _, other := range ag.agents {
			tutor := other.thread.Kind == workspace.TaskKind || other.thread.Kind == workspace.LessonKind
			if tutor && other != ta && other.key.ws == w.ID && other.pending == 0 {
				ag.end(other)
			}
		}
	}
	return ta
}

// loop opens the agent's CLI, then hands it the queued messages one turn at
// a time until the agent ends.
func (ag *Agents) loop(ctx context.Context, w *workspace.Workspace, ta *threadAgent) {
	defer func() {
		ag.mu.Lock()
		ag.end(ta)
		ag.mu.Unlock()
	}()
	tr := newTranscript(w, ta.key.name)
	cli, err := ag.open(ctx, w, ta, tr.handle)
	if err == nil {
		ta.cli = cli
	}
	close(ta.ready)
	if err != nil {
		// The messages waiting for this start are dropped, and the failure
		// is posted once. The agent leaves the agents before its thread
		// shows idle, so the next message tries a new start.
		cancelled := ctx.Err() != nil // the workspace was deleted or the app quit
		ag.mu.Lock()
		ag.end(ta)
		ag.setPending(ta, 0)
		ag.mu.Unlock()
		if !cancelled {
			postFailure(w, ta.key.name, err.Error())
		}
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ta.queue:
			err := ta.cli.Prompt(ctx, msg)
			tr.endTurn()
			if ta.stopped.Swap(false) {
				// After endTurn, so the notice follows the cut-off reply.
				Post(w, workspace.ChatItem{Thread: ta.key.name, Role: workspace.RoleNotice, Text: "Stopped. Send a message to continue."})
			}
			ended := ag.answered(ta, errors.Is(err, agent.ErrExited))
			if err != nil && ctx.Err() == nil {
				postFailure(w, ta.key.name, err.Error())
			}
			if ended {
				return
			}
		}
	}
}

// open opens the CLI session for a thread's agent.
func (ag *Agents) open(ctx context.Context, w *workspace.Workspace, ta *threadAgent, onEvent func(agent.Event)) (agent.Session, error) {
	// This also switches the workspace to the agent in the settings, which
	// forgets the other agent's saved conversations.
	saved, err := w.UseProvider(ta.cfg.Provider)
	if err != nil {
		return nil, err
	}
	// A writer starts fresh every time, so its conversation isn't kept.
	resume := ta.thread.Kind != workspace.WriterKind
	opts := agent.Options{
		Cwd: w.Dir, MCPName: mcp.Name, MCPURL: ag.mcp.URL(w, ta.thread, ag), Instructions: instructions(w, ta.thread),
		Model: ta.cfg.Model, Effort: ta.cfg.Effort, Subagents: usesSubagents(ta.thread.Kind),
	}
	if resume {
		opts.ResumeID = saved[ta.key.name]
	}
	cli, err := ag.cli.Start(ctx, ta.cfg.Provider, opts, onEvent)
	// Saved before the first turn, so the id is on disk even if the app
	// quits during it.
	if err == nil && resume {
		_ = w.SetResumeID(ta.cfg.Provider, ta.key.name, cli.ID())
	}
	return cli, err
}

// usesSubagents reports whether a kind of thread may hand research to
// subagents. The plan and writers may; task and lesson chats answer a
// learner who's waiting, so they don't.
func usesSubagents(kind workspace.ThreadKind) bool {
	return kind == workspace.PlanKind || kind == workspace.WriterKind
}

// answered counts one of an agent's messages as answered, or given up on.
// The agent ends if its CLI exited, which loses any queued messages, and a
// writer's ends once it's idle. An ending agent leaves the agents before its
// thread shows idle, so the next message starts a new one instead of
// queueing on this one. It reports whether the agent ended.
func (ag *Agents) answered(ta *threadAgent, exited bool) bool {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	n := ta.pending - 1
	ended := exited || n == 0 && ta.thread.Kind == workspace.WriterKind
	if ended {
		n = 0
		if ag.agents[ta.key] == ta {
			delete(ag.agents, ta.key)
		}
	}
	ag.setPending(ta, n)
	return ended
}

// setPending sets how many messages an agent has left, and tells the UI
// when its thread turns busy or idle. Call with ag.mu held.
func (ag *Agents) setPending(ta *threadAgent, n int) {
	was := ta.pending > 0
	ta.pending = n
	if busy := n > 0; busy != was {
		ag.seq++
		events.Emit(events.ThreadBusy, events.Busy{Workspace: ta.key.ws, Thread: ta.key.name, Busy: busy, Seq: ag.seq})
	}
}

// end removes a thread's agent and closes its CLI. Call with ag.mu held.
func (ag *Agents) end(ta *threadAgent) {
	if ag.agents[ta.key] == ta {
		delete(ag.agents, ta.key)
	}
	ta.close()
}
