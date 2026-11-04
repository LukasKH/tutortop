package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Queueing doesn't wait for the agent to start. A thread is busy from the
// moment a message is queued, through its agent's start, until every queued
// message is answered.
func TestBusyFromStartUntilQueueIsDone(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	busy := func() bool { b, _, _ := ag.state(w.ID); return b["plan"] }

	fake.holdStart = make(chan struct{})
	queue(t, ag, w, workspace.PlanThread, "one")
	if !busy() {
		t.Fatal("idle while starting")
	}
	close(fake.holdStart)
	first := fake.next(t)
	queue(t, ag, w, workspace.PlanThread, "two")
	first.end(nil)
	second := fake.next(t)
	if second.text != "two" || second.s != first.s {
		t.Fatalf("second turn = %q on a new session: %v", second.text, second.s != first.s)
	}
	if !busy() {
		t.Fatal("idle between queued turns")
	}
	second.end(nil)
	eventually(t, "idle once answered", func() bool { return !busy() })
}

// A writer starts fresh, isn't queued twice, keeps its sandbox check while
// it works and ends after its turn; the next message starts a new writer.
func TestWriterEndsAfterItsTurn(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	writer := workspace.WriterThread("go.errors")

	queue(t, ag, w, writer, "write")
	turn := fake.next(t)
	if turn.s.opts.ResumeID != "" {
		t.Fatalf("writer resumed %q", turn.s.opts.ResumeID)
	}
	queue(t, ag, w, writer, "write again")
	ag.showCheck(w.ID, SandboxCheck{Kind: CheckTask, Thread: writer.String(), Title: "Wrap errors", Running: true})
	if busy, checks, _ := ag.state(w.ID); !busy[writer.String()] || !checks["go.errors"].Running {
		t.Fatalf("busy %v, checks %v", busy, checks)
	}
	turn.end(nil)
	eventually(t, "writer gone", func() bool {
		busy, checks, _ := ag.state(w.ID)
		return len(busy) == 0 && len(checks) == 0 && turn.s.closed.Load()
	})
	if saved, _ := w.UseProvider("claude"); len(saved) != 0 {
		t.Fatalf("a writer's resume id was saved: %v", saved)
	}

	queue(t, ag, w, writer, "write")
	if again := fake.next(t); again.s == turn.s || again.text != "write" {
		t.Fatalf("got %q on the first writer: %v", again.text, again.s == turn.s)
	}
}

// New settings restart a thread's agent once it's idle, resuming its
// conversation, but not while it works.
func TestSettingsRestartIdleSession(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	queue(t, ag, w, workspace.PlanThread, "one")
	first := fake.next(t)
	fake.cfg.Model = "other"
	queue(t, ag, w, workspace.PlanThread, "two")
	first.end(nil)
	second := fake.next(t)
	if second.s != first.s {
		t.Fatal("restarted a busy session")
	}
	second.end(nil)
	eventually(t, "idle", func() bool { b, _, _ := ag.state(w.ID); return !b["plan"] })

	queue(t, ag, w, workspace.PlanThread, "three")
	third := fake.next(t)
	if third.s == first.s || !first.s.closed.Load() {
		t.Fatal("the idle session wasn't restarted")
	}
	if third.s.opts.Model != "other" || third.s.opts.ResumeID != first.s.ID() {
		t.Fatalf("restarted with model %q, resuming %q", third.s.opts.Model, third.s.opts.ResumeID)
	}
}

// Opening a lesson closes the idle task tutors, but not a busy one or the plan.
func TestIdleTutorsCloseWhenAnotherStarts(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	open := func(thread workspace.Thread) *heldSession {
		t.Helper()
		queue(t, ag, w, thread, "hi")
		return fake.next(t).s
	}
	answer := func(thread workspace.Thread, s *heldSession) {
		t.Helper()
		s.turn <- nil
		eventually(t, thread.String()+" idle", func() bool { b, _, _ := ag.state(w.ID); return !b[thread.String()] })
	}
	busyTask := open(workspace.TaskThread("0001-busy"))
	idleTask := open(workspace.TaskThread("0002-idle"))
	answer(workspace.TaskThread("0002-idle"), idleTask)
	plan := open(workspace.PlanThread)
	answer(workspace.PlanThread, plan)

	open(workspace.LessonThread("0001-errors"))
	if !idleTask.closed.Load() || busyTask.closed.Load() || plan.closed.Load() {
		t.Fatalf("closed: idle task %v, busy task %v, plan %v", idleTask.closed.Load(), busyTask.closed.Load(), plan.closed.Load())
	}
	// The closed tutor's thread starts a new one when it's used again.
	if again := open(workspace.TaskThread("0002-idle")); again == idleTask {
		t.Fatal("reused a closed session")
	}
}

// Stop cuts the turn short; the notice comes after what was said so far.
func TestStopPostsNoticeAfterReply(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	queue(t, ag, w, workspace.PlanThread, "explain")
	turn := fake.next(t)
	turn.s.onEvent(agent.Event{Kind: agent.EventText, Text: "Well, "})
	ag.stop(w.ID, "plan")
	eventually(t, "stopped", func() bool { c, _ := w.Chat("plan"); return len(c) == 2 })
	chat, _ := w.Chat("plan")
	if chat[0].Text != "Well, " || !strings.HasPrefix(chat[1].Text, "Stopped.") {
		t.Fatalf("chat = %+v", chat)
	}
}

// When the agent can't start or its process exits, the thread shows the
// error once in the chat and goes idle, and the next message starts a new
// agent.
func TestFailedAgentIsReplaced(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	idle := func() bool { b, _, _ := ag.state(w.ID); return !b["plan"] }
	fake.holdStart = make(chan struct{})
	fake.startErr = errors.New("claude isn't installed")
	queue(t, ag, w, workspace.PlanThread, "one")
	queue(t, ag, w, workspace.PlanThread, "one again")
	close(fake.holdStart)
	eventually(t, "idle after a failed start", idle)
	fake.startErr = nil
	queue(t, ag, w, workspace.PlanThread, "two")
	crashed := fake.next(t)
	crashed.end(fmt.Errorf("%w: out of memory", agent.ErrStopped))
	eventually(t, "idle after the crash", idle)
	queue(t, ag, w, workspace.PlanThread, "three")
	if fake.next(t).s == crashed.s {
		t.Fatal("queued on a stopped agent")
	}
	// One for the failed start, though two messages waited for it, and one
	// for the crash.
	failures := func() (n int) {
		chat, _ := w.Chat("plan")
		for _, c := range chat {
			if c.Status == workspace.ChatFailed {
				n++
			}
		}
		return n
	}
	eventually(t, "failures shown", func() bool { return failures() >= 2 })
	if n := failures(); n != 2 {
		t.Fatalf("%d failures shown", n)
	}
}

// newHeldAgents is a workspace's thread agents whose turns last until the
// test ends them.
func newHeldAgents(t *testing.T) (*threadAgents, *heldAgents, *workspace.Workspace) {
	t.Helper()
	fake := &heldAgents{turns: make(chan heldTurn, 8), cfg: agentConfig{Agent: "claude"}}
	ag := newThreadAgents(fake.start, func() agentConfig { return fake.cfg }, func(*workspace.Workspace, workspace.Thread) agent.Options { return agent.Options{} })
	t.Cleanup(ag.endAll)
	store := &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	w, err := store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	return ag, fake, w
}

// queue queues a message for a thread's agent.
func queue(t *testing.T, ag *threadAgents, w *workspace.Workspace, thread workspace.Thread, msg string) {
	t.Helper()
	if err := ag.queue(w, thread, msg); err != nil {
		t.Fatal(err)
	}
}

// heldAgents fakes the agent processes the thread agents open, and the
// settings they read.
type heldAgents struct {
	cfg       agentConfig   // the settings; change them only between queue calls
	holdStart chan struct{} // if set, starts wait until it's closed
	startErr  error         // if set, starts fail
	started   atomic.Int32
	turns     chan heldTurn // turns that have started
}

func (f *heldAgents) start(ctx context.Context, _ string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error) {
	if f.holdStart != nil {
		<-f.holdStart
	}
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &heldSession{agents: f, id: fmt.Sprint("session-", f.started.Add(1)), opts: opts, onEvent: onEvent, turn: make(chan error, 1), cancel: make(chan struct{}, 1)}, nil
}

// next waits for the next turn to start.
func (f *heldAgents) next(t *testing.T) heldTurn {
	t.Helper()
	select {
	case turn := <-f.turns:
		return turn
	case <-time.After(2 * time.Second):
		t.Fatal("no turn started")
		return heldTurn{}
	}
}

type heldTurn struct {
	s    *heldSession
	text string
}

// end makes the turn's Prompt return err.
func (turn heldTurn) end(err error) { turn.s.turn <- err }

type heldSession struct {
	agents  *heldAgents
	id      string
	opts    agent.Options
	onEvent func(agent.Event)
	turn    chan error // the current turn returns what's sent here; buffered, so ending a turn that's gone doesn't hang the test
	cancel  chan struct{}
	closed  atomic.Bool
}

func (s *heldSession) Prompt(ctx context.Context, text string) error {
	s.agents.turns <- heldTurn{s, text}
	select {
	case err := <-s.turn:
		return err
	case <-s.cancel:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *heldSession) ID() string { return s.id }

func (s *heldSession) Cancel() {
	select {
	case s.cancel <- struct{}{}:
	default:
	}
}

func (s *heldSession) Close() error { s.closed.Store(true); return nil }

// planned gives a workspace an approved curriculum with these objectives.
func planned(t *testing.T, w *workspace.Workspace, objectives ...string) {
	t.Helper()
	m := workspace.Module{ID: "m", Title: "M", References: []workspace.Source{{Title: "Go docs", URL: "https://go.dev/doc/"}}}
	for _, id := range objectives {
		m.Objectives = append(m.Objectives, workspace.Objective{ID: id, Name: id, Title: id})
	}
	if err := w.Propose(workspace.Curriculum{Language: "go", Modules: []workspace.Module{m}}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
}

// eventually waits up to two seconds for cond to hold.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting: %s", what)
		}
	}
}
