package threads

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/mcp"
	"github.com/LukasKH/tutortop/internal/mcp/mcptest"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/settings"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Queueing doesn't wait for the agent to start. A thread is busy from the
// moment a message is queued, through its agent's start, until every queued
// message is answered.
func TestBusyFromStartUntilQueueIsDone(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	busy := func() bool { b, _, _ := ag.State(w.ID); return b["plan"] }

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

	// An agent ended mid-turn shows idle too.
	shown := recordBusy(t)
	queue(t, ag, w, workspace.PlanThread, "three")
	fake.next(t)
	ag.EndWorkspace(w.ID)
	if busy() || shown.last("plan") != "idle" {
		t.Fatalf("after the workspace ended: busy %v, last event %q", busy(), shown.last("plan"))
	}
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
	ag.Show(w.ID, events.SandboxCheck{Kind: events.CheckTask, Thread: writer.String(), Title: "Wrap errors", Running: true})
	if busy, checks, _ := ag.State(w.ID); !busy[writer.String()] || !checks["go.errors"].Running {
		t.Fatalf("busy %v, checks %v", busy, checks)
	}
	turn.end(nil)
	eventually(t, "writer gone", func() bool {
		busy, checks, _ := ag.State(w.ID)
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

// A writer that shows no sign of life is ended and its thread shows idle,
// with a failure on its objective, so it can be written again. A running
// sandbox check doesn't count as stuck.
func TestStuckWriterIsEnded(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	busy := recordBusy(t)
	ag.writerIdle = 50 * time.Millisecond
	writer := workspace.WriterThread("go.errors")

	queue(t, ag, w, writer, "write")
	stuck := fake.next(t)
	ag.Show(w.ID, events.SandboxCheck{Kind: events.CheckTask, Thread: writer.String(), Title: "Wrap errors", Running: true})
	time.Sleep(4 * ag.writerIdle)
	if b, _, _ := ag.State(w.ID); !b[writer.String()] {
		t.Fatal("ended during a sandbox check")
	}
	ag.Show(w.ID, events.SandboxCheck{Kind: events.CheckTask, Thread: writer.String(), Title: "Wrap errors"})
	eventually(t, "stuck writer ended", func() bool {
		b, _, _ := ag.State(w.ID)
		return !b[writer.String()] && stuck.s.closed.Load() && busy.last(writer.String()) == "idle"
	})
	eventually(t, "failure shown", func() bool { return failures(w, writer.String()) == 1 })

	ag.writerIdle = time.Minute
	queue(t, ag, w, writer, "write")
	again := fake.next(t)
	if again.s == stuck.s {
		t.Fatal("retried on the stuck writer")
	}
	again.end(nil)
	eventually(t, "retry done", func() bool { b, _, _ := ag.State(w.ID); return !b[writer.String()] })
}

// Stopping a writer ends it at once, while it works or while its CLI is
// still opening, without a failure; the next message starts a new one.
func TestStoppedWriterEnds(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	busy := recordBusy(t)
	writer := workspace.WriterThread("go.errors")
	idle := func() bool { b, _, _ := ag.State(w.ID); return !b[writer.String()] }

	queue(t, ag, w, writer, "write")
	turn := fake.next(t)
	ag.stop(w.ID, writer.String())
	if !idle() || !turn.s.closed.Load() || busy.last(writer.String()) != "idle" {
		t.Fatalf("after stop: idle %v, closed %v, last event %q", idle(), turn.s.closed.Load(), busy.last(writer.String()))
	}

	fake.holdStart = make(chan struct{})
	queue(t, ag, w, writer, "write")
	ag.stop(w.ID, writer.String())
	if !idle() {
		t.Fatal("busy after stopping while opening")
	}
	close(fake.holdStart)

	queue(t, ag, w, writer, "write")
	again := fake.next(t)
	if again.s == turn.s || again.text != "write" {
		t.Fatalf("got %q on the stopped writer: %v", again.text, again.s == turn.s)
	}
	again.end(nil)
	eventually(t, "idle", idle)
	if n := failures(w, writer.String()); n != 0 {
		t.Fatalf("%d failures shown for a stop", n)
	}
}

// A writer's first message hands it its objective's sources: the module's
// references and the learner's resources.
func TestWriterGetsItsSources(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	ref := workspace.Source{Title: "Go blog: Working with Errors", URL: "https://go.dev/blog/go1.13-errors"}
	o := workspace.Objective{ID: "go.errors", Name: "Wrapping errors", Title: "Wrap errors"}
	plan := workspace.Curriculum{
		Language: "go",
		Modules: []workspace.Module{{
			ID:         "errors",
			Title:      "Errors",
			References: []workspace.Source{ref},
			Objectives: []workspace.Objective{o},
		}},
	}
	if err := w.Propose(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Dir, "RESOURCES.md"), []byte("# Resources\n\n- [Effective Go](https://go.dev/doc/effective_go): idioms\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	NewWriters(ag).Write(w, o, "The learner just approved the plan.")
	turn := fake.next(t)
	for _, want := range []string{"[context]", "[" + ref.Title + "](" + ref.URL + ")", "[Effective Go](https://go.dev/doc/effective_go)", "Write for the objective “Wrap errors” (go.errors)"} {
		if !strings.Contains(turn.text, want) {
			t.Errorf("writer's message lacks %q:\n%s", want, turn.text)
		}
	}
	turn.end(nil)
}

// New settings restart a thread's agent once it's idle, resuming its
// conversation, but not while it works.
func TestSettingsRestartIdleSession(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	queue(t, ag, w, workspace.PlanThread, "one")
	first := fake.next(t)
	if err := ag.settings.Save(settings.Prefs{Provider: "claude", Models: map[string]settings.ModelChoice{"claude": {Model: "other"}}}); err != nil {
		t.Fatal(err)
	}
	queue(t, ag, w, workspace.PlanThread, "two")
	first.end(nil)
	second := fake.next(t)
	if second.s != first.s {
		t.Fatal("restarted a busy session")
	}
	second.end(nil)
	eventually(t, "idle", func() bool { b, _, _ := ag.State(w.ID); return !b["plan"] })

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
		eventually(t, thread.String()+" idle", func() bool { b, _, _ := ag.State(w.ID); return !b[thread.String()] })
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
	if chat[0].Role != workspace.RoleAssistant || chat[0].Text != "Well, " || chat[1].Role != workspace.RoleNotice {
		t.Fatalf("chat = %+v", chat)
	}
}

// When the agent can't start or its process exits, the thread shows the
// error once in the chat and goes idle, and the next message starts a new
// agent.
func TestFailedAgentIsReplaced(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	idle := func() bool { b, _, _ := ag.State(w.ID); return !b["plan"] }
	fake.holdStart = make(chan struct{})
	fake.startErr = errors.New("claude isn't installed")
	queue(t, ag, w, workspace.PlanThread, "one")
	queue(t, ag, w, workspace.PlanThread, "one again")
	close(fake.holdStart)
	eventually(t, "idle after a failed start", idle)
	fake.startErr = nil
	queue(t, ag, w, workspace.PlanThread, "two")
	crashed := fake.next(t)
	crashed.end(fmt.Errorf("%w: out of memory", agent.ErrExited))
	eventually(t, "idle after the crash", idle)
	queue(t, ag, w, workspace.PlanThread, "three")
	if fake.next(t).s == crashed.s {
		t.Fatal("queued on a stopped agent")
	}
	// One for the failed start, though two messages waited for it, and one
	// for the crash.
	eventually(t, "failures shown", func() bool { return failures(w, "plan") >= 2 })
	if n := failures(w, "plan"); n != 2 {
		t.Fatalf("%d failures shown", n)
	}
}

// newHeldAgents is a workspace's thread agents whose turns last until the
// test ends them.
func newHeldAgents(t *testing.T) (*Agents, *heldAgents, *workspace.Workspace) {
	t.Helper()
	fake := &heldAgents{turns: make(chan heldTurn, 8)}
	ag, store := newTestAgents(t, fake)
	if err := ag.settings.Save(settings.Prefs{Provider: "claude"}); err != nil {
		t.Fatal(err)
	}
	w, err := store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	return ag, fake, w
}

// newTestAgents runs thread agents on cli, with the settings and the
// workspaces in store in temp dirs, and links that all open. They end with
// the test.
func newTestAgents(t *testing.T, cli CLI) (*Agents, *workspace.Store) {
	t.Helper()
	tools, err := mcp.Start(sandbox.New(), mcptest.Links())
	if err != nil {
		t.Fatal(err)
	}
	ag := NewAgents(settings.NewStore(filepath.Join(t.TempDir(), "settings.yaml")), tools, cli)
	t.Cleanup(ag.endAll)
	return ag, newTestStore(t)
}

// newTestStore keeps a test's workspaces in temp dirs.
func newTestStore(t *testing.T) *workspace.Store {
	return &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
}

// newTestWorkspace is a new workspace in a newTestStore.
func newTestWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	w, err := newTestStore(t).Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// queue queues a message for a thread's agent.
func queue(t *testing.T, ag *Agents, w *workspace.Workspace, thread workspace.Thread, msg string) {
	t.Helper()
	if err := ag.queue(w, thread, msg); err != nil {
		t.Fatal(err)
	}
}

// heldAgents fakes the agent CLI the thread agents start.
type heldAgents struct {
	holdStart chan struct{} // if set, starts wait until it's closed
	startErr  error         // if set, starts fail
	started   atomic.Int32
	turns     chan heldTurn // turns that have started
}

func (f *heldAgents) Start(ctx context.Context, _ string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error) {
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

// failures counts the failures shown in a thread's chat.
func failures(w *workspace.Workspace, thread string) (n int) {
	chat, _ := w.Chat(thread)
	for _, c := range chat {
		if c.Status == workspace.ChatFailed {
			n++
		}
	}
	return n
}

// busyEvents records the thread:busy events sent during a test.
type busyEvents struct {
	mu     sync.Mutex
	latest map[string]bool // by thread
}

// recordBusy records the thread:busy events until the test ends.
func recordBusy(t *testing.T) *busyEvents {
	t.Helper()
	b := &busyEvents{latest: map[string]bool{}}
	events.SetEmitter(func(_ string, data any) {
		if e, ok := data.(events.Busy); ok {
			b.mu.Lock()
			b.latest[e.Thread] = e.Busy
			b.mu.Unlock()
		}
	})
	t.Cleanup(func() { events.SetEmitter(nil) })
	return b
}

// last is what the last event for a thread said: "busy", "idle", or "" if none came.
func (b *busyEvents) last(thread string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	busy, ok := b.latest[thread]
	switch {
	case !ok:
		return ""
	case busy:
		return "busy"
	}
	return "idle"
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
