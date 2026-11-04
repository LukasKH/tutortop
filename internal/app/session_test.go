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

// Sending doesn't wait for the agent to start. A thread is busy from the
// moment a message is sent, through its agent's start, until every queued
// message is answered.
func TestBusyFromStartUntilQueueIsDone(t *testing.T) {
	a, agents, w := newHeldApp(t)
	busy := func() bool { b, _ := a.activity(w.ID); return b["plan"] }

	agents.holdStart = make(chan struct{})
	if err := a.SendMessage(w.ID, "one", "plan"); err != nil {
		t.Fatal(err)
	}
	if !busy() {
		t.Fatal("idle while starting")
	}
	close(agents.holdStart)
	first := agents.next(t)
	if err := a.SendMessage(w.ID, "two", "plan"); err != nil {
		t.Fatal(err)
	}
	first.end(nil)
	second := agents.next(t)
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
// it works and ends after its turn; the next request starts a new writer.
func TestWriterEndsAfterItsTurn(t *testing.T) {
	a, agents, w := newHeldApp(t)
	planned(t, w, "go.errors")

	if err := a.PrepareObjective(w.ID, "go.errors"); err != nil {
		t.Fatal(err)
	}
	turn := agents.next(t)
	if !strings.Contains(turn.text, "go.errors") || turn.s.opts.ResumeID != "" {
		t.Fatalf("writer got %q, resuming %q", turn.text, turn.s.opts.ResumeID)
	}
	if err := a.PrepareObjective(w.ID, "go.errors"); err != nil {
		t.Fatal(err)
	}
	a.writerCheck(w.ID, SandboxCheck{Kind: CheckTask, Thread: "write-go.errors", Title: "Wrap errors", Running: true})
	if st, _ := a.Workspace(w.ID); !st.Busy["write-go.errors"] || !st.Checks["go.errors"].Running {
		t.Fatalf("busy %v, checks %v", st.Busy, st.Checks)
	}
	turn.end(nil)
	eventually(t, "writer gone", func() bool {
		st, _ := a.Workspace(w.ID)
		return len(st.Busy) == 0 && len(st.Checks) == 0 && turn.s.closed.Load()
	})
	if w2, _ := a.store.Open(w.ID); len(w2.Meta.Sessions) != 0 {
		t.Fatalf("a writer's session was saved: %v", w2.Meta.Sessions)
	}

	if err := a.PrepareObjective(w.ID, "go.errors"); err != nil {
		t.Fatal(err)
	}
	if again := agents.next(t); again.s == turn.s {
		t.Fatal("the second request was queued on the first writer")
	}
}

// New settings restart a session once it's idle, resuming its conversation,
// but not while it works.
func TestSettingsRestartIdleSession(t *testing.T) {
	a, agents, w := newHeldApp(t)
	if err := a.SendMessage(w.ID, "one", "plan"); err != nil {
		t.Fatal(err)
	}
	first := agents.next(t)
	s := a.Settings()
	s.Tutor[s.Agent] = TutorChoice{Model: "other"}
	if err := a.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	if err := a.SendMessage(w.ID, "two", "plan"); err != nil {
		t.Fatal(err)
	}
	first.end(nil)
	second := agents.next(t)
	if second.s != first.s {
		t.Fatal("restarted a busy session")
	}
	second.end(nil)
	eventually(t, "idle", func() bool { b, _ := a.activity(w.ID); return !b["plan"] })

	if err := a.SendMessage(w.ID, "three", "plan"); err != nil {
		t.Fatal(err)
	}
	third := agents.next(t)
	if third.s == first.s || !first.s.closed.Load() {
		t.Fatal("the idle session wasn't restarted")
	}
	if third.s.opts.Model != "other" || third.s.opts.ResumeID != first.s.ID() {
		t.Fatalf("restarted with model %q, resuming %q", third.s.opts.Model, third.s.opts.ResumeID)
	}
}

// Opening a lesson closes the idle task tutors, but not a busy one or the plan.
func TestIdleTutorsCloseWhenAnotherStarts(t *testing.T) {
	a, agents, w := newHeldApp(t)
	open := func(thread string) *heldSession {
		t.Helper()
		if err := a.SendMessage(w.ID, "hi", thread); err != nil {
			t.Fatal(err)
		}
		return agents.next(t).s
	}
	answer := func(thread string, s *heldSession) {
		t.Helper()
		s.turn <- nil
		eventually(t, thread+" idle", func() bool { b, _ := a.activity(w.ID); return !b[thread] })
	}
	busyTask := open("0001-busy")
	idleTask := open("0002-idle")
	answer("0002-idle", idleTask)
	plan := open("plan")
	answer("plan", plan)

	open(workspace.LessonThread("0001-errors").String())
	if !idleTask.closed.Load() || busyTask.closed.Load() || plan.closed.Load() {
		t.Fatalf("closed: idle task %v, busy task %v, plan %v", idleTask.closed.Load(), busyTask.closed.Load(), plan.closed.Load())
	}
	// The closed tutor's thread starts a new one when it's used again.
	if again := open("0002-idle"); again == idleTask {
		t.Fatal("reused a closed session")
	}
}

// Stop cuts the turn short; the notice comes after what was said so far.
func TestStopPostsNoticeAfterReply(t *testing.T) {
	a, agents, w := newHeldApp(t)
	if err := a.SendMessage(w.ID, "explain", "plan"); err != nil {
		t.Fatal(err)
	}
	turn := agents.next(t)
	turn.s.onEvent(agent.Event{Kind: agent.EventText, Text: "Well, "})
	a.StopAgent(w.ID, "plan")
	eventually(t, "stopped", func() bool { c, _ := a.Chat(w.ID, "plan"); return len(c) == 3 })
	chat, _ := a.Chat(w.ID, "plan")
	if chat[1].Text != "Well, " || !strings.HasPrefix(chat[2].Text, "Stopped.") {
		t.Fatalf("chat = %+v", chat)
	}
}

// When the agent can't start or its process exits, the thread shows the
// error once in the chat and goes idle, and the next message starts a new
// agent.
func TestFailedAgentIsReplaced(t *testing.T) {
	a, agents, w := newHeldApp(t)
	idle := func() bool { b, _ := a.activity(w.ID); return !b["plan"] }
	agents.holdStart = make(chan struct{})
	agents.startErr = errors.New("claude isn't installed")
	for _, text := range []string{"one", "one again"} {
		if err := a.SendMessage(w.ID, text, "plan"); err != nil {
			t.Fatal(err)
		}
	}
	close(agents.holdStart)
	eventually(t, "idle after a failed start", idle)
	agents.startErr = nil
	if err := a.SendMessage(w.ID, "two", "plan"); err != nil {
		t.Fatal(err)
	}
	crashed := agents.next(t)
	crashed.end(fmt.Errorf("%w: out of memory", agent.ErrStopped))
	eventually(t, "idle after the crash", idle)
	if err := a.SendMessage(w.ID, "three", "plan"); err != nil {
		t.Fatal(err)
	}
	if agents.next(t).s == crashed.s {
		t.Fatal("queued on a stopped agent")
	}
	// One for the failed start, though two messages waited for it, and one
	// for the crash.
	failures := func() (n int) {
		chat, _ := a.Chat(w.ID, "plan")
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

// newHeldApp is a test app whose agents' turns last until the test ends them.
func newHeldApp(t *testing.T) (*App, *heldAgents, *workspace.Workspace) {
	t.Helper()
	a := newTestApp(t)
	agents := &heldAgents{turns: make(chan heldTurn, 8)}
	a.startAgent = agents.start
	t.Cleanup(func() { _ = a.ServiceShutdown() })
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	return a, agents, w
}

// heldAgents fakes the agent processes the app starts.
type heldAgents struct {
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
