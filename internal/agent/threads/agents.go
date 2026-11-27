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

// Agents runs each thread's agent, at most one per thread. Each verb means
// one thing: a thread agent starts and ends, its CLI session opens and
// closes, a message is queued and then answered, and stop interrupts the
// current turn. docs/architecture.md ("Thread agents") has the lifecycle.
type Agents struct {
	settings *settings.Store // the agent, model and effort, read for every message
	mcp      *mcp.Server
	cli      CLI

	// writerIdle is how long a writer may show no sign of life before it's
	// ended (watchdog). Tests shorten it.
	writerIdle time.Duration

	mu     sync.Mutex
	agents map[threadKey]*threadAgent
	seq    uint64 // numbers busy and check changes and snapshots in order
}

// writerIdleLimit is long enough for a model that thinks hard between tool calls;
// a sandbox check, whose image build can take longer, pauses the watchdog.
const writerIdleLimit = 10 * time.Minute

var _ mcp.Checks = (*Agents)(nil)

// NewAgents runs thread agents on the agent, model and effort in the
// settings, opening their CLI sessions with cli. Each thread's tools are
// served by tools.
func NewAgents(settingsStore *settings.Store, tools *mcp.Server, cli CLI) *Agents {
	return &Agents{
		settings:   settingsStore,
		mcp:        tools,
		cli:        cli,
		writerIdle: writerIdleLimit,
		agents:     map[threadKey]*threadAgent{},
		seq:        firstSeq(),
	}
}

// Message is one thing sent to the tutor. What the learner sees in the chat
// (Shown) can differ from what the tutor receives (Agent): the app attaches
// the open task as context, and its own instructions to the tutor are shown
// as short notices.
type Message struct {
	Role   workspace.ChatRole // workspace.RoleUser for the learner's words, RoleNotice for app events
	Shown  string
	Agent  string
	Images []workspace.ChatImage // pictures the learner attached, shown and sent with the text
	After  func()                // optional callback run once this turn completes without error or stop
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

// Send posts a message in a thread's chat and queues it for that thread's
// agent, without waiting for the agent to start. Once the message is in the
// chat, whatever goes wrong with it is posted there too.
func (ag *Agents) Send(w *workspace.Workspace, t workspace.Thread, m Message) {
	_ = ag.TrySend(w, t, m) // posted in the chat
}

// TrySend is Send that also returns why the agent couldn't take the message,
// for a caller that lets the learner try again. A CLI that fails to open
// fails later, in the chat only.
func (ag *Agents) TrySend(w *workspace.Workspace, t workspace.Thread, m Message) error {
	item := workspace.ChatItem{Thread: t.String(), Role: m.Role, Text: m.Shown}
	p := prompt{text: m.Agent, after: m.After}
	for _, img := range m.Images {
		item.Images = append(item.Images, img.Name)
		p.images = append(p.images, agent.Image{Path: img.Path, MIMEType: img.MIMEType})
	}
	Post(w, item)
	err := ag.queue(w, t, p)
	if err != nil {
		postFailure(w, t.String(), err.Error())
	}
	return err
}

// The UI learns which threads are busy, and how writers' sandbox checks go,
// from both snapshots (State) and events (setPending, StartCheck). All three
// take the next number from ag.seq under ag.mu, so the numbers order them
// exactly as they happened: the frontend keeps whichever is newer, and a
// snapshot read before an event doesn't undo it.

// StartCheck tells the UI a sandbox check has started (mcp.Checks) and
// returns a Check that reports it finished. A writer's check is kept on its
// threadAgent so the objective's page shows it even after the learner looks
// away, and goes when the writer does; a finish call from an ended writer is
// ignored so it can't overwrite a retry's check or watchdog.
func (ag *Agents) StartCheck(wsID string, c events.SandboxCheck) mcp.Check {
	key := threadKey{wsID, c.Thread}
	c.Running = true
	ag.mu.Lock()
	owner := ag.agents[key]
	if c.Kind == events.CheckTask && owner == nil {
		ag.mu.Unlock()
		return func(bool) {} // writer already ended
	}
	if owner != nil {
		owner.pauseWatch()
		if c.Kind == events.CheckTask {
			owner.check = &c
		}
	}
	ag.seq++
	events.Emit(events.CheckStatus, events.Check{Workspace: wsID, Check: c, Seq: ag.seq})
	ag.mu.Unlock()

	var once sync.Once
	return func(passed bool) {
		once.Do(func() {
			done := c
			done.Running, done.Passed = false, passed
			ag.mu.Lock()
			defer ag.mu.Unlock()
			ta := ag.agents[key]
			if done.Kind == events.CheckTask && ta != owner {
				return // writer ended or was retried: don't touch the new one
			}
			if owner != nil && ta == owner {
				ta.resumeWatch()
				if done.Kind == events.CheckTask {
					ta.check = &done
				}
			}
			ag.seq++
			events.Emit(events.CheckStatus, events.Check{Workspace: wsID, Check: done, Seq: ag.seq})
		})
	}
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

// SupersedeTurn flushes any already-streamed output from an in-flight turn in t
// and drops the rest of that turn's output so closing prose after ask_user or
// propose_curriculum cannot appear after the learner's response or approval.
func (ag *Agents) SupersedeTurn(wsID string, t workspace.Thread) {
	ag.mu.Lock()
	ta := ag.agents[threadKey{wsID, t.String()}]
	ag.mu.Unlock()
	if ta != nil {
		ta.tr.supersedeTurn()
	}
}

// endWriters stops every running writer for wsID, drops their MCP endpoints,
// and returns the objective IDs that were being written so Rewrite can restart
// requested writes (such as "Write it now") alongside cleared ones.
func (ag *Agents) endWriters(wsID string) []string {
	ag.mu.Lock()
	var stopped []string
	for key, ta := range ag.agents {
		if key.ws == wsID && ta.thread.Kind == workspace.WriterKind {
			stopped = append(stopped, ta.thread.ID)
			ag.end(ta)
		}
	}
	ag.mu.Unlock()
	ag.mcp.ForgetWriters(wsID)
	return stopped
}

// queue queues a message for a thread's agent, starting the agent if it
// isn't running. It doesn't wait for the start: the CLI opens in the
// background, and if it doesn't, one notice is posted in the thread's chat.
// It fails only if too many messages are already waiting.
func (ag *Agents) queue(w *workspace.Workspace, t workspace.Thread, msg prompt) error {
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
// once the cut-off reply is saved, and takes the next message as usual. If a
// chat thread's CLI is still opening, stop ends the startup immediately and
// posts the notice so the queued prompt doesn't run once it opens.
// A writer has only the one turn, so stop ends it instead, even while its
// CLI is still opening or doesn't answer an interrupt.
func (ag *Agents) stop(wsID, thread string) {
	ag.mu.Lock()
	ta := ag.agents[threadKey{wsID, thread}]
	if ta == nil {
		ag.mu.Unlock()
		return
	}
	if ta.thread.Kind == workspace.WriterKind {
		ag.end(ta)
		ag.mu.Unlock()
		return
	}
	if ta.pending == 0 {
		ag.mu.Unlock()
		return
	}
	if !ta.opened() {
		Post(ta.tr.ws, workspace.ChatItem{Thread: ta.key.name, Role: workspace.RoleNotice, Text: "Stopped. Send a message to continue."})
		ag.end(ta)
		ag.mu.Unlock()
		return
	}
	ag.mu.Unlock()
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

// start starts a thread's agent. Call with ag.mu held.
func (ag *Agents) start(w *workspace.Workspace, t workspace.Thread, cfg agentConfig) *threadAgent {
	ctx, cancel := context.WithCancel(context.Background())
	ta := &threadAgent{
		key:    threadKey{w.ID, t.String()},
		thread: t,
		cfg:    cfg,
		tr:     newTranscript(w, t.String()),
		queue:  make(chan prompt, 32),
		cancel: cancel,
		ready:  make(chan struct{}),
	}
	if t.Kind == workspace.WriterKind {
		ta.started = time.Now()
		ta.idle = ag.writerIdle
		ta.watchCh = make(chan struct{}, 1)
		go ag.watchWriter(ctx, w, ta)
	}
	ag.agents[ta.key] = ta
	go ag.loop(ctx, w, ta)
	if t.Kind == workspace.PlanKind {
		return ta
	}
	for _, other := range ag.agents {
		tutor := other.thread.Kind == workspace.TaskKind || other.thread.Kind == workspace.LessonKind
		if tutor && other != ta && other.key.ws == w.ID && other.pending == 0 {
			ag.end(other)
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
	cli, err := ag.open(ctx, w, ta, func(ev agent.Event) {
		ta.alive()
		ta.tr.handle(ev)
	})
	if err == nil {
		ta.cli = cli
	}
	close(ta.ready)
	if err == nil && ctx.Err() != nil {
		// Ended while it opened, so close may have found no CLI to close.
		// Closing twice is harmless.
		_ = cli.Close()
		return
	}
	if err != nil {
		// The messages waiting for this start are dropped, and the failure
		// is posted once. The agent leaves the agents before its thread
		// shows idle, so the next message tries a new start.
		cancelled := ctx.Err() != nil // ended: the workspace was deleted, the app quit, or a writer was stopped
		ag.mu.Lock()
		ag.end(ta)
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
			if ag.runTurn(ctx, w, ta, msg) {
				return
			}
		}
	}
}

// runTurn answers one queued message and reports whether the agent ended.
func (ag *Agents) runTurn(ctx context.Context, w *workspace.Workspace, ta *threadAgent, msg prompt) bool {
	if ta.stopped.Swap(false) {
		Post(w, workspace.ChatItem{Thread: ta.key.name, Role: workspace.RoleNotice, Text: "Stopped. Send a message to continue."})
		return ag.finishTurn(ta, cliRunning)
	}
	ta.tr.startTurn()
	err := ta.cli.Prompt(ctx, msg.text, msg.images)
	ta.tr.endTurn()
	stopped := ta.stopped.Swap(false)
	if stopped {
		// After endTurn, so the notice follows the cut-off reply.
		Post(w, workspace.ChatItem{Thread: ta.key.name, Role: workspace.RoleNotice, Text: "Stopped. Send a message to continue."})
	}
	if err == nil && !stopped && ctx.Err() == nil && ta.thread.Kind == workspace.WriterKind {
		err = ag.finishWriterTurn(ctx, w, ta)
	}
	// Run After before marking the turn idle so replacement writers
	// are queued before the UI (or tests) sees the plan thread idle.
	if err == nil && !stopped && ctx.Err() == nil && msg.after != nil {
		msg.after()
	}
	state := cliRunning
	if errors.Is(err, agent.ErrExited) {
		state = cliExited
	}
	ended := ag.finishTurn(ta, state)
	if err != nil && ctx.Err() == nil {
		postFailure(w, ta.key.name, err.Error())
	}
	return ended
}

// open opens the CLI session for a thread's agent.
func (ag *Agents) open(ctx context.Context, w *workspace.Workspace, ta *threadAgent, onEvent func(agent.Event)) (agent.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// This also switches the workspace to the agent in the settings, which
	// forgets the other agent's saved conversations.
	saved, err := w.UseProvider(ta.cfg.Provider)
	if err != nil {
		return nil, err
	}
	// A writer starts fresh every time, so its conversation isn't kept.
	resume := ta.thread.Kind != workspace.WriterKind
	opts := agent.Options{
		Cwd:          w.Dir,
		MCPName:      mcp.Name,
		MCPURL:       ag.mcp.URL(w, ta.thread, ag),
		Instructions: instructions(w, ta.thread),
		Binary:       ta.cfg.Binary,
		Model:        ta.cfg.Model,
		Effort:       ta.cfg.Effort,
		Subagents:    usesSubagents(ta.thread.Kind),
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

type cliState bool

const (
	cliRunning cliState = false
	cliExited  cliState = true
)

// finishTurn counts one of an agent's messages as answered, or given up on.
// The agent ends if its CLI exited, which loses any queued messages, and a
// writer's ends once it's idle. An ending agent leaves the agents before its
// thread shows idle, so the next message starts a new one instead of
// queueing on this one. It reports whether the agent ended.
func (ag *Agents) finishTurn(ta *threadAgent, state cliState) bool {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	if ag.agents[ta.key] != ta {
		return true // already ended, and shown idle
	}
	n := ta.pending - 1
	ended := state == cliExited || n == 0 && ta.thread.Kind == workspace.WriterKind
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

// watchWriter ends a writer that shows no sign of life for ta.idle: no event
// from its CLI and no running sandbox check.
func (ag *Agents) watchWriter(ctx context.Context, w *workspace.Workspace, ta *threadAgent) {
	t := time.NewTimer(ta.idle)
	defer t.Stop()
	rearm := func() {
		if ta.checks.Load() > 0 {
			t.Stop()
			return
		}
		t.Reset(max(0, ta.idle-ta.idleTime()))
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ta.watchCh:
			rearm()
		case <-t.C:
			if ag.stalled(w, ta) {
				return
			}
			rearm()
		}
	}
}

// stalled ends a writer its watchdog found stuck, and says why in its
// thread, which the objective's page shows. Posting before end() under ag.mu
// orders the notice ahead of busy:false so a quick retry clears it cleanly.
// It reports whether the writer is done (ended now or already gone).
func (ag *Agents) stalled(w *workspace.Workspace, ta *threadAgent) bool {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	if ag.agents[ta.key] != ta {
		return true
	}
	if ta.checks.Load() > 0 || ta.idleTime() < ta.idle {
		return false // raced with alive() or a sandbox check
	}
	postFailure(w, ta.key.name, "The writer stopped making progress, so it was stopped.")
	ag.end(ta)
	return true
}

// finishWriterTurn verifies that a writer published both parts of its
// objective. If it published only one part (e.g. the lesson without the
// exercise), it nudges the same session once to publish the missing part; if
// anything is still missing afterwards, it posts a failure notice so the UI
// prompts the learner to retry instead of silently leaving it unwritten.
func (ag *Agents) finishWriterTurn(ctx context.Context, w *workspace.Workspace, ta *threadAgent) error {
	missingLesson, missingTask := writerMissing(w, ta.thread.ID)
	if !missingLesson && !missingTask {
		return nil
	}
	if missingLesson != missingTask {
		nudge := "[app] You published the lesson, but not the exercise yet. Call create_task now."
		if missingLesson {
			nudge = "[app] You published the exercise, but not the lesson yet. Call create_lesson now."
		}
		ta.tr.startTurn()
		err := ta.cli.Prompt(ctx, nudge, nil)
		ta.tr.endTurn()
		if err != nil || ctx.Err() != nil {
			return err
		}
		missingLesson, missingTask = writerMissing(w, ta.thread.ID)
	}
	if missingLesson || missingTask {
		postFailure(w, ta.key.name, missingWriterMessage(missingLesson, missingTask))
	}
	return nil
}

// end removes a thread's agent, closes its CLI and shows its thread idle,
// however the agent ends. Call with ag.mu held.
func (ag *Agents) end(ta *threadAgent) {
	if ag.agents[ta.key] == ta {
		delete(ag.agents, ta.key)
	}
	ta.close()
	ag.setPending(ta, 0)
}

// A threadAgent is one thread's running agent: its CLI session and the
// queue of messages it answers, one turn at a time, in order.
type threadAgent struct {
	key     threadKey
	thread  workspace.Thread
	cfg     agentConfig // the agent, CLI, model and effort it was started with
	tr      *transcript
	queue   chan prompt
	cancel  context.CancelFunc
	once    sync.Once   // close runs once
	stopped atomic.Bool // the learner pressed Stop during the current turn

	ready chan struct{} // closed once the CLI has opened, or failed to
	cli   agent.Session // set before ready is closed; nil if it didn't open

	// Writer watchdog state (watchCh is nil for chat agents, which the learner watches and can stop).
	watchCh   chan struct{} // buffered(1): wakes watchWriter on activity or check changes
	started   time.Time     // monotonic origin for lastAlive
	idle      time.Duration // copied from Agents.writerIdle when the writer starts
	lastAlive atomic.Int64  // time since started of the last sign of life
	checks    atomic.Int32  // running sandbox checks pausing the watchdog

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

// close closes the CLI and its transcript, if it's open or opening.
func (ta *threadAgent) close() {
	ta.once.Do(func() {
		ta.cancel()
		ta.tr.close()
		if ta.opened() {
			_ = ta.cli.Close()
		}
	})
}

// alive records a sign of life from a writer's CLI.
func (ta *threadAgent) alive() {
	if ta.watchCh == nil {
		return
	}
	ta.lastAlive.Store(int64(time.Since(ta.started)))
	ta.pingWatch()
}

// pauseWatch pauses a writer's watchdog while a sandbox check runs.
func (ta *threadAgent) pauseWatch() {
	if ta.watchCh == nil {
		return
	}
	ta.checks.Add(1)
	ta.pingWatch()
}

// resumeWatch resumes a writer's watchdog once a sandbox check finishes.
func (ta *threadAgent) resumeWatch() {
	if ta.watchCh == nil {
		return
	}
	ta.lastAlive.Store(int64(time.Since(ta.started)))
	ta.checks.Add(-1)
	ta.pingWatch()
}

func (ta *threadAgent) pingWatch() {
	select {
	case ta.watchCh <- struct{}{}:
	default:
	}
}

func (ta *threadAgent) idleTime() time.Duration {
	return time.Since(ta.started) - time.Duration(ta.lastAlive.Load())
}

// prompt is one queued message, as the agent receives it.
type prompt struct {
	text   string
	images []agent.Image
	after  func()
}

// threadKey names one thread of one workspace by the thread's name.
type threadKey struct{ ws, name string }

// agentConfig is what a thread's agent is started with. One started with an
// older config restarts once it's idle.
type agentConfig struct {
	Provider string
	Binary   string // the CLI's path from the settings; "" finds it on PATH
	Model    string
	Effort   string
}

// configOf is the agent, CLI, model and effort the settings choose.
func configOf(s settings.Prefs) agentConfig {
	c := s.Models[s.Provider]
	return agentConfig{Provider: s.Provider, Binary: s.Agents[s.Provider].Path, Model: c.Model, Effort: c.Effort}
}

// firstSeq is where Agents.seq starts. Counting from the time in
// microseconds keeps the numbers rising across a restart, for a page that
// outlives one in server mode.
func firstSeq() uint64 {
	return uint64(time.Now().UnixMicro())
}

// writerMissing reports whether an unpassed objective in w's curriculum is
// missing a lesson or an exercise.
func writerMissing(w *workspace.Workspace, objectiveID string) (missingLesson, missingTask bool) {
	cur, err := w.Curriculum()
	if err != nil || cur == nil {
		return false, false
	}
	if _, _, ok := cur.Objective(objectiveID); !ok {
		return false, false
	}
	p := w.Progress()
	op := p.Objectives[objectiveID]
	if op.Status == workspace.ObjectivePassed {
		return false, false
	}
	lessons, _ := w.Lessons()
	hasLesson := false
	for _, l := range lessons {
		if l.Objective == objectiveID {
			hasLesson = true
			break
		}
	}
	return !hasLesson, len(op.Tasks) == 0
}

func missingWriterMessage(missingLesson, missingTask bool) string {
	switch {
	case missingLesson && missingTask:
		return "The writer finished without publishing the lesson or exercise."
	case missingLesson:
		return "The writer finished without publishing the lesson."
	default:
		return "The writer finished without publishing the exercise."
	}
}

// usesSubagents reports whether a kind of thread may hand research to
// subagents. The plan and writers may; task and lesson chats answer a
// learner who's waiting, so they don't.
func usesSubagents(kind workspace.ThreadKind) bool {
	switch kind {
	case workspace.PlanKind, workspace.WriterKind:
		return true
	case workspace.TaskKind, workspace.LessonKind:
		return false
	}
	return false
}
