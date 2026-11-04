package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// A session is one thread's agent process. It answers the thread's messages
// one turn at a time, in order.
//
//   - It starts with the first message queued for its thread (enqueue). A
//     writer's agent starts a fresh conversation; any other resumes the
//     thread's saved one.
//   - Its thread is busy from the moment a message is queued until the agent
//     has answered every queued message. That includes the start, which can
//     take several seconds, so the learner sees the tutor working meanwhile.
//   - Once idle, it restarts with the next message if the settings changed.
//   - It ends when its process exits, its workspace is deleted or the app
//     quits. A writer also ends as soon as it's idle: its writing is done.
//     And starting any thread but the plan ends the workspace's idle task and
//     lesson sessions, so moving through tasks doesn't pile up processes;
//     their conversations resume if they're reopened.
type session struct {
	key    threadKey
	thread workspace.Thread
	cfg    tutorConfig // the agent, model and effort it was started with
	queue  chan string
	cancel context.CancelFunc
	once   sync.Once   // close runs once
	stop   atomic.Bool // the learner pressed Stop during the current turn

	ready chan struct{} // closed once the agent has started, or failed to
	agent agent.Session // set before ready is closed
	err   error         // why the agent didn't start; set before ready is closed

	// Guarded by App.mu.
	pending int   // messages queued or being answered; the thread is busy while it's above 0
	gate    *Gate // a writer's latest sandbox check of its task
}

// started reports whether the agent process is up, without blocking.
func (s *session) started() bool {
	select {
	case <-s.ready:
		return s.agent != nil
	default:
		return false
	}
}

// close stops the agent process, if it's started or starting.
func (s *session) close() {
	s.once.Do(func() {
		s.cancel()
		if s.started() {
			_ = s.agent.Close()
		}
	})
}

// threadKey names one thread of one workspace by the thread's name.
type threadKey struct{ ws, name string }

// enqueue queues a message for a thread's agent, starting the agent if it
// isn't running, and returns once the agent has started. With ifIdle it
// queues nothing while the thread is busy.
func (a *App) enqueue(w *workspace.Workspace, t workspace.Thread, msg string, ifIdle bool) error {
	s := a.claim(w, t, ifIdle)
	if s == nil {
		return nil
	}
	<-s.ready
	if s.err != nil {
		a.release(s, false)
		return s.err
	}
	s.queue <- msg
	return nil
}

// claim returns a thread's session, starting one if needed, and counts one
// more message for it. The process starts in the background, so a slow start
// doesn't hold a.mu; callers wait on ready for the same start.
func (a *App) claim(w *workspace.Workspace, t workspace.Thread, ifIdle bool) *session {
	key := threadKey{w.ID, t.String()}
	cfg := a.Settings().tutor()
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[key]
	if s != nil && s.pending > 0 && ifIdle {
		return nil
	}
	// The settings changed since it started: restart it with the new ones.
	// A busy session finishes its queue first.
	if s != nil && s.pending == 0 && s.cfg != cfg {
		a.end(s)
		s = nil
	}
	if s == nil {
		s = a.start(w, t, cfg)
	}
	a.setPending(s, s.pending+1)
	return s
}

// start starts a thread's session. Call with a.mu held.
func (a *App) start(w *workspace.Workspace, t workspace.Thread, cfg tutorConfig) *session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{key: threadKey{w.ID, t.String()}, thread: t, cfg: cfg, queue: make(chan string, 32), cancel: cancel, ready: make(chan struct{})}
	a.sessions[s.key] = s
	go a.run(ctx, w, s)
	if t.Kind != workspace.PlanKind {
		for _, other := range a.sessions {
			tutor := other.thread.Kind == workspace.TaskKind || other.thread.Kind == workspace.LessonKind
			if tutor && other != s && other.key.ws == w.ID && other.pending == 0 {
				a.end(other)
			}
		}
	}
	return s
}

// run starts the session's agent, then hands it the queued messages one turn
// at a time until the session ends.
func (a *App) run(ctx context.Context, w *workspace.Workspace, s *session) {
	defer func() {
		a.mu.Lock()
		a.end(s)
		a.mu.Unlock()
	}()
	tr := &transcript{app: a, ws: w, thread: s.key.name, tools: map[string]*workspace.ChatItem{}}
	s.agent, s.err = a.launch(ctx, w, s, tr.handle)
	if s.err != nil {
		// Out of the sessions before its callers see the error, so the next
		// message tries a new start rather than getting this error again.
		a.mu.Lock()
		a.end(s)
		a.mu.Unlock()
		close(s.ready)
		return
	}
	close(s.ready)
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.queue:
			err := s.agent.Prompt(ctx, msg)
			tr.endTurn()
			if s.stop.Swap(false) {
				// After endTurn, so the notice follows the cut-off reply.
				a.post(w, workspace.ChatItem{Thread: s.key.name, Role: workspace.RoleNotice, Text: "Stopped. Send a message to continue."})
			}
			ended := a.release(s, errors.Is(err, agent.ErrStopped))
			if err != nil && ctx.Err() == nil {
				a.postFailure(w, s.key.name, err.Error())
			}
			if ended {
				return
			}
		}
	}
}

// launch starts the agent process for a session.
func (a *App) launch(ctx context.Context, w *workspace.Workspace, s *session, onEvent func(agent.Event)) (agent.Session, error) {
	// This also switches the workspace to the agent in the settings, which
	// forgets the other agent's saved conversations.
	saved, err := w.UseProvider(s.cfg.Agent)
	if err != nil {
		return nil, err
	}
	// A writer starts fresh every time, so its conversation isn't kept.
	resume := s.thread.Kind != workspace.WriterKind
	opts := agent.Options{
		Cwd:          w.Dir,
		MCPName:      mcpName,
		MCPURL:       a.mcp.URL(w, s.key.name),
		Instructions: instructions(w, s.thread),
		Model:        s.cfg.Model,
		Effort:       s.cfg.Effort,
	}
	if resume {
		opts.ResumeID = saved[s.key.name]
	}
	sess, err := a.startAgent(ctx, s.cfg.Agent, opts, onEvent)
	// Saved before ready closes, so the session id is on disk once the first
	// message is queued.
	if err == nil && resume {
		_ = w.SetSession(s.cfg.Agent, s.key.name, sess.ID())
	}
	return sess, err
}

// release counts one of a session's messages as answered, or given up on.
// The session ends if its process exited, which loses any queued messages,
// and a writer's ends once it's idle. An ending session leaves the sessions
// before its thread shows idle, so the next message starts a new one instead
// of queueing on this one. It reports whether the session ended.
func (a *App) release(s *session, exited bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := s.pending - 1
	ended := exited || n == 0 && s.thread.Kind == workspace.WriterKind
	if ended {
		n = 0
		if a.sessions[s.key] == s {
			delete(a.sessions, s.key)
		}
	}
	a.setPending(s, n)
	return ended
}

// setPending sets how many messages a session has left, and tells the UI
// when its thread turns busy or idle. Call with a.mu held, so the events go
// out in the order the changes happened.
func (a *App) setPending(s *session, n int) {
	was := s.pending > 0
	s.pending = n
	if busy := n > 0; busy != was {
		emit(threadBusy, BusyEvent{Workspace: s.key.ws, Thread: s.key.name, Busy: busy})
	}
}

// end removes a session and stops its process. Call with a.mu held.
func (a *App) end(s *session) {
	if a.sessions[s.key] == s {
		delete(a.sessions, s.key)
	}
	s.close()
}

// activity returns which of a workspace's threads are busy, and its writers'
// latest sandbox checks by objective.
func (a *App) activity(wsID string) (busy map[string]bool, checks map[string]Gate) {
	busy, checks = map[string]bool{}, map[string]Gate{}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.sessions {
		if s.key.ws != wsID {
			continue
		}
		if s.pending > 0 {
			busy[s.key.name] = true
		}
		if s.gate != nil {
			checks[s.thread.ID] = *s.gate
		}
	}
	return busy, checks
}
