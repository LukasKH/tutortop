package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// threadAgents runs each thread's agent, at most one per thread. Its methods
// are all the rest of the app uses: queue a message for a thread, stop a
// thread's turn, show a sandbox check, read what the agents are doing, and
// end a workspace's agents or all of them.
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
type threadAgents struct {
	// What the agents need from the rest of the app: a way to open an
	// agent's CLI session (agent.Start; tests pass a fake), the settings'
	// agent, model and effort (read for every message), and a thread's
	// folder, MCP server and instructions.
	openSession sessionOpener
	config      func() agentConfig
	options     func(*workspace.Workspace, workspace.Thread) agent.Options

	mu     sync.Mutex
	agents map[threadKey]*threadAgent
	seq    uint64 // numbers busy and check changes and snapshots in order
}

// sessionOpener is agent.Start's signature.
type sessionOpener func(ctx context.Context, provider string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error)

func newThreadAgents(openSession sessionOpener, config func() agentConfig, options func(*workspace.Workspace, workspace.Thread) agent.Options) *threadAgents {
	return &threadAgents{openSession: openSession, config: config, options: options, agents: map[threadKey]*threadAgent{},
		// Counting from the time in microseconds keeps the numbers rising
		// across a restart, for a page that outlives one in server mode.
		seq: uint64(time.Now().UnixMicro())}
}

// queue queues a message for a thread's agent, starting the agent if it
// isn't running. It doesn't wait for the start: the CLI opens in the
// background, and if it doesn't, one notice is posted in the thread's chat.
// It fails only if too many messages are already waiting.
func (ag *threadAgents) queue(w *workspace.Workspace, t workspace.Thread, msg string) error {
	key := threadKey{w.ID, t.String()}
	cfg := ag.config()
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
func (ag *threadAgents) stop(wsID, thread string) {
	ag.mu.Lock()
	ta := ag.agents[threadKey{wsID, thread}]
	ag.mu.Unlock()
	if ta == nil || !ta.opened() {
		return
	}
	ta.stopped.Store(true)
	ta.cli.Cancel()
}

// The UI learns which threads are busy, and how writers' sandbox checks go,
// from both snapshots (state) and events (setPending, showCheck). All three
// take the next number from ag.seq under ag.mu, so the numbers order them
// exactly as they happened: the frontend keeps whichever is newer, and a
// snapshot read before an event doesn't undo it.

// showCheck tells the UI how a sandbox check is going. A writer's is also
// kept on its threadAgent, so the objective's page shows it even after the
// learner looks away; it goes when the writer does.
func (ag *threadAgents) showCheck(wsID string, c SandboxCheck) {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	if ta := ag.agents[threadKey{wsID, c.Thread}]; ta != nil && c.Kind == CheckTask {
		ta.check = &c
	}
	ag.seq++
	emit(checkStatus, CheckEvent{Workspace: wsID, Check: c, Seq: ag.seq})
}

// state returns what the workspace's agents are doing: which threads are
// busy, and the writers' latest sandbox checks by objective. Its seq also
// orders snapshots among themselves.
func (ag *threadAgents) state(wsID string) (busy map[string]bool, checks map[string]SandboxCheck, seq uint64) {
	busy, checks = map[string]bool{}, map[string]SandboxCheck{}
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

// endWorkspace ends a workspace's agents, before it's deleted.
func (ag *threadAgents) endWorkspace(wsID string) {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	for key, ta := range ag.agents {
		if key.ws == wsID {
			ag.end(ta)
		}
	}
}

// endAll ends every agent, as the app quits.
func (ag *threadAgents) endAll() {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	for _, ta := range ag.agents {
		ag.end(ta)
	}
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

	// Guarded by threadAgents.mu.
	pending int           // messages queued or being answered; the thread is busy while it's above 0
	check   *SandboxCheck // a writer's latest sandbox check of its task
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
func (ag *threadAgents) start(w *workspace.Workspace, t workspace.Thread, cfg agentConfig) *threadAgent {
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
func (ag *threadAgents) loop(ctx context.Context, w *workspace.Workspace, ta *threadAgent) {
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
				post(w, workspace.ChatItem{Thread: ta.key.name, Role: workspace.RoleNotice, Text: "Stopped. Send a message to continue."})
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
func (ag *threadAgents) open(ctx context.Context, w *workspace.Workspace, ta *threadAgent, onEvent func(agent.Event)) (agent.Session, error) {
	// This also switches the workspace to the agent in the settings, which
	// forgets the other agent's saved conversations.
	saved, err := w.UseProvider(ta.cfg.Provider)
	if err != nil {
		return nil, err
	}
	// A writer starts fresh every time, so its conversation isn't kept.
	resume := ta.thread.Kind != workspace.WriterKind
	opts := ag.options(w, ta.thread)
	opts.Model, opts.Effort = ta.cfg.Model, ta.cfg.Effort
	if resume {
		opts.ResumeID = saved[ta.key.name]
	}
	cli, err := ag.openSession(ctx, ta.cfg.Provider, opts, onEvent)
	// Saved before the first turn, so the id is on disk even if the app
	// quits during it.
	if err == nil && resume {
		_ = w.SetResumeID(ta.cfg.Provider, ta.key.name, cli.ID())
	}
	return cli, err
}

// answered counts one of an agent's messages as answered, or given up on.
// The agent ends if its CLI exited, which loses any queued messages, and a
// writer's ends once it's idle. An ending agent leaves the agents before its
// thread shows idle, so the next message starts a new one instead of
// queueing on this one. It reports whether the agent ended.
func (ag *threadAgents) answered(ta *threadAgent, exited bool) bool {
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
func (ag *threadAgents) setPending(ta *threadAgent, n int) {
	was := ta.pending > 0
	ta.pending = n
	if busy := n > 0; busy != was {
		ag.seq++
		emit(threadBusy, BusyEvent{Workspace: ta.key.ws, Thread: ta.key.name, Busy: busy, Seq: ag.seq})
	}
}

// end removes a thread's agent and closes its CLI. Call with ag.mu held.
func (ag *threadAgents) end(ta *threadAgent) {
	if ag.agents[ta.key] == ta {
		delete(ag.agents, ta.key)
	}
	ta.close()
}
