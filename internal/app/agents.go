package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// A threadAgent is one thread's running agent: the agent's CLI session and
// the queue of messages it answers, one turn at a time, in order.
//
//   - It starts with the first message queued for its thread (enqueue), in
//     the background: queueing doesn't wait for the start. A writer's agent
//     starts a fresh conversation; any other resumes the thread's saved one.
//   - Its thread is busy from the moment a message is queued until the agent
//     has answered every queued message. That includes the start, which can
//     take several seconds, so the learner sees the tutor working meanwhile.
//   - If the agent doesn't start, it posts why in the thread's chat, once,
//     drops the waiting messages and ends; the thread turns idle.
//   - Once idle, it restarts with the next message if the settings changed.
//   - It ends when its process exits, its workspace is deleted or the app
//     quits. A writer also ends as soon as it's idle: its writing is done.
//     And starting any thread but the plan ends the workspace's idle task and
//     lesson agents, so moving through tasks doesn't pile up processes;
//     their conversations resume if they're reopened.
type threadAgent struct {
	key    threadKey
	thread workspace.Thread
	cfg    agentConfig // the agent, model and effort it was started with
	queue  chan string
	cancel context.CancelFunc
	once   sync.Once   // close runs once
	stop   atomic.Bool // the learner pressed Stop during the current turn

	ready chan struct{} // closed once the CLI has started, or failed to
	cli   agent.Session // set before ready is closed; nil if the start failed

	// Guarded by App.mu.
	pending int           // messages queued or being answered; the thread is busy while it's above 0
	check   *SandboxCheck // a writer's latest sandbox check of its task
}

// started reports whether the agent process is up, without blocking.
func (ta *threadAgent) started() bool {
	select {
	case <-ta.ready:
		return ta.cli != nil
	default:
		return false
	}
}

// close stops the agent process, if it's started or starting.
func (ta *threadAgent) close() {
	ta.once.Do(func() {
		ta.cancel()
		if ta.started() {
			_ = ta.cli.Close()
		}
	})
}

// threadKey names one thread of one workspace by the thread's name.
type threadKey struct{ ws, name string }

// enqueue queues a message for a thread's agent, starting the agent if it
// isn't running. It doesn't wait for the start: the process starts in the
// background, and a start that fails posts one notice in the thread's chat.
// With ifIdle it queues nothing while the thread is busy. It fails only if
// too many messages are already waiting.
func (a *App) enqueue(w *workspace.Workspace, t workspace.Thread, msg string, ifIdle bool) error {
	key := threadKey{w.ID, t.String()}
	cfg := a.Settings().agentConfig()
	a.mu.Lock()
	defer a.mu.Unlock()
	ta := a.agents[key]
	if ta != nil && ta.pending > 0 && ifIdle {
		return nil
	}
	// The settings changed since it started: restart it with the new ones.
	// A busy agent finishes its queue first.
	if ta != nil && ta.pending == 0 && ta.cfg != cfg {
		a.end(ta)
		ta = nil
	}
	if ta == nil {
		ta = a.start(w, t, cfg)
	}
	// Queued under a.mu, so an agent that ends can't take a message after
	// it has left the agents.
	select {
	case ta.queue <- msg:
	default:
		return errors.New("too many messages are waiting; let the tutor catch up first")
	}
	a.setPending(ta, ta.pending+1)
	return nil
}

// start starts a thread's agent. Call with a.mu held.
func (a *App) start(w *workspace.Workspace, t workspace.Thread, cfg agentConfig) *threadAgent {
	ctx, cancel := context.WithCancel(context.Background())
	ta := &threadAgent{key: threadKey{w.ID, t.String()}, thread: t, cfg: cfg, queue: make(chan string, 32), cancel: cancel, ready: make(chan struct{})}
	a.agents[ta.key] = ta
	go a.run(ctx, w, ta)
	if t.Kind != workspace.PlanKind {
		for _, other := range a.agents {
			tutor := other.thread.Kind == workspace.TaskKind || other.thread.Kind == workspace.LessonKind
			if tutor && other != ta && other.key.ws == w.ID && other.pending == 0 {
				a.end(other)
			}
		}
	}
	return ta
}

// run starts the agent's CLI, then hands it the queued messages one turn at
// a time until the agent ends.
func (a *App) run(ctx context.Context, w *workspace.Workspace, ta *threadAgent) {
	defer func() {
		a.mu.Lock()
		a.end(ta)
		a.mu.Unlock()
	}()
	tr := &transcript{app: a, ws: w, thread: ta.key.name, tools: map[string]*workspace.ChatItem{}}
	cli, err := a.launch(ctx, w, ta, tr.handle)
	if err == nil {
		ta.cli = cli
	}
	close(ta.ready)
	if err != nil {
		// The messages waiting for this start are dropped, and the failure
		// is posted once. The agent leaves the agents before its thread
		// shows idle, so the next message tries a new start.
		cancelled := ctx.Err() != nil // the workspace was deleted or the app quit
		a.mu.Lock()
		a.end(ta)
		a.setPending(ta, 0)
		a.mu.Unlock()
		if !cancelled {
			a.postFailure(w, ta.key.name, err.Error())
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
			if ta.stop.Swap(false) {
				// After endTurn, so the notice follows the cut-off reply.
				a.post(w, workspace.ChatItem{Thread: ta.key.name, Role: workspace.RoleNotice, Text: "Stopped. Send a message to continue."})
			}
			ended := a.release(ta, errors.Is(err, agent.ErrStopped))
			if err != nil && ctx.Err() == nil {
				a.postFailure(w, ta.key.name, err.Error())
			}
			if ended {
				return
			}
		}
	}
}

// launch starts the CLI for a thread's agent.
func (a *App) launch(ctx context.Context, w *workspace.Workspace, ta *threadAgent, onEvent func(agent.Event)) (agent.Session, error) {
	// This also switches the workspace to the agent in the settings, which
	// forgets the other agent's saved conversations.
	saved, err := w.UseProvider(ta.cfg.Agent)
	if err != nil {
		return nil, err
	}
	// A writer starts fresh every time, so its conversation isn't kept.
	resume := ta.thread.Kind != workspace.WriterKind
	opts := agent.Options{
		Cwd:          w.Dir,
		MCPName:      mcpName,
		MCPURL:       a.mcp.URL(w, ta.thread),
		Instructions: instructions(w, ta.thread),
		Model:        ta.cfg.Model,
		Effort:       ta.cfg.Effort,
	}
	if resume {
		opts.ResumeID = saved[ta.key.name]
	}
	cli, err := a.startAgent(ctx, ta.cfg.Agent, opts, onEvent)
	// Saved before the first turn, so the id is on disk even if the app
	// quits during it.
	if err == nil && resume {
		_ = w.SetResumeID(ta.cfg.Agent, ta.key.name, cli.ID())
	}
	return cli, err
}

// release counts one of an agent's messages as answered, or given up on.
// The agent ends if its process exited, which loses any queued messages,
// and a writer's ends once it's idle. An ending agent leaves the agents
// before its thread shows idle, so the next message starts a new one instead
// of queueing on this one. It reports whether the agent ended.
func (a *App) release(ta *threadAgent, exited bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := ta.pending - 1
	ended := exited || n == 0 && ta.thread.Kind == workspace.WriterKind
	if ended {
		n = 0
		if a.agents[ta.key] == ta {
			delete(a.agents, ta.key)
		}
	}
	a.setPending(ta, n)
	return ended
}

// setPending sets how many messages an agent has left, and tells the UI
// when its thread turns busy or idle. Call with a.mu held, so the events go
// out in the order the changes happened.
func (a *App) setPending(ta *threadAgent, n int) {
	was := ta.pending > 0
	ta.pending = n
	if busy := n > 0; busy != was {
		emit(threadBusy, BusyEvent{Workspace: ta.key.ws, Thread: ta.key.name, Busy: busy})
	}
}

// end removes a thread's agent and stops its process. Call with a.mu held.
func (a *App) end(ta *threadAgent) {
	if a.agents[ta.key] == ta {
		delete(a.agents, ta.key)
	}
	ta.close()
}

// agentState returns what the workspace's agents are doing: which threads
// are busy, and the writers' latest sandbox checks by objective.
func (a *App) agentState(wsID string) (busy map[string]bool, checks map[string]SandboxCheck) {
	busy, checks = map[string]bool{}, map[string]SandboxCheck{}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ta := range a.agents {
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
	return busy, checks
}
