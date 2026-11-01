package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/text"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// threadKey names one chat thread of one workspace: "plan", a task id, a
// lesson's thread, or an objective's writer.
type threadKey struct{ ws, thread string }

// agentSession is one live coach process for one chat thread.
// Messages are handled one turn at a time, in order.
type agentSession struct {
	thread  string
	cfg     coachConfig // the agent, model and effort it was started with
	queue   chan string
	cancel  context.CancelFunc
	busy    bool
	stopped atomic.Bool // the learner pressed Stop during the current turn
	once    sync.Once

	ready    chan struct{} // closed once the agent has started (or failed to)
	sess     agent.Session // set before ready is closed
	startErr error
}

// started reports whether the agent process is up, without blocking.
func (s *agentSession) started() bool {
	select {
	case <-s.ready:
		return s.sess != nil
	default:
		return false
	}
}

func (s *agentSession) close() {
	s.once.Do(func() {
		s.cancel()
		if s.started() {
			_ = s.sess.Close()
		}
	})
}

// Chat returns one thread's transcript: "plan" or a task id.
func (a *App) Chat(wsID, thread string) ([]workspace.ChatItem, error) {
	w, err := a.store.Open(wsID)
	if err != nil {
		return nil, err
	}
	return w.Chat(thread)
}

// SendMessage sends the learner's chat message to a thread. In a task's
// thread the app adds a [context] block with their code and last check result.
func (a *App) SendMessage(wsID, text, thread string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	thread = cmp.Or(thread, workspace.PlanThread)
	return a.send(w, thread, message{Role: workspace.RoleUser, Shown: text, Agent: a.taskContext(w, thread) + text})
}

// AnswerQuestions sends the learner's answers to the thread that asked.
func (a *App) AnswerQuestions(wsID string, answers map[string]string) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	thread := workspace.PlanThread
	var b strings.Builder
	b.WriteString("My answers:\n")
	if q := w.PendingQuestions(); q != nil {
		thread = cmp.Or(q.Thread, thread)
		for _, qu := range q.Questions {
			fmt.Fprintf(&b, "- %s\n  → %s\n", qu.Question, answers[qu.ID])
		}
	} else {
		for _, k := range slices.Sorted(maps.Keys(answers)) {
			fmt.Fprintf(&b, "- %s: %s\n", k, answers[k])
		}
	}
	if err := w.SetQuestions(nil); err != nil {
		return err
	}
	return a.send(w, thread, message{Role: workspace.RoleUser, Shown: b.String(), Agent: b.String()})
}

// DismissQuestions drops the pending form without answering.
func (a *App) DismissQuestions(wsID string) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	return w.SetQuestions(nil)
}

// RequestHint records the hint (it counts against mastery) and asks the
// task's tutor for it.
func (a *App) RequestHint(wsID, taskID string, level int) error {
	if level < 1 || level > 4 {
		return errors.New("hint level must be 1-4")
	}
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	_ = w.RecordAttempt(workspace.Attempt{TaskID: taskID, Kind: workspace.KindHint, HintLevel: level})
	names := []string{"", "a nudge", "a pointer", "a snippet", "the solution walkthrough"}
	msg := fmt.Sprintf("Hint level %d (%s), please.", level, names[level])
	return a.send(w, taskID, message{Role: workspace.RoleUser, Shown: msg, Agent: a.taskContext(w, taskID) + msg})
}

// StopAgent interrupts the current turn in a thread.
func (a *App) StopAgent(wsID, thread string) {
	a.mu.Lock()
	s := a.sessions[threadKey{wsID, thread}]
	a.mu.Unlock()
	if s == nil || !s.started() {
		return
	}
	s.stopped.Store(true)
	s.sess.Cancel()
}

// taskContext describes the learner's current code and last check for a
// task thread; the plan thread gets none.
func (a *App) taskContext(w *workspace.Workspace, thread string) string {
	if thread == workspace.PlanThread {
		return ""
	}
	if id, ok := workspace.LessonOfThread(thread); ok {
		return lessonContext(w, id)
	}
	d, err := w.Task(thread)
	if err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[context]\nOpen task: %s (%s), objective %s, language %s.\n", d.ID, d.Title, d.Objective, d.Language)
	for _, name := range slices.Sorted(maps.Keys(d.Work)) {
		fmt.Fprintf(&b, "Learner's current %s:\n```\n%s\n```\n", name, d.Work[name])
	}
	a.mu.Lock()
	last, ok := a.lastResults[threadKey{w.ID, d.ID}]
	a.mu.Unlock()
	if ok {
		p, t := last.Counts()
		fmt.Fprintf(&b, "Last check: %s, %d/%d tests passing.\n", last.Status, p, t)
		for _, tr := range last.Tests {
			if !tr.Passed {
				kind := "visible"
				if tr.Hidden {
					kind = "hidden"
				}
				fmt.Fprintf(&b, "- %s (%s) failed: %s\n", tr.Name, kind, text.Truncate(tr.Message, 400))
			}
		}
		if last.Status == runner.StatusError {
			fmt.Fprintf(&b, "Output:\n%s\n", text.Truncate(last.Output, 1500))
		}
	}
	b.WriteString("[/context]\n\n")
	return b.String()
}

// message is one thing sent to the coach. What the learner sees in the chat
// (Shown) can differ from what the coach receives (Agent): the app attaches
// the open task as context, and its own instructions to the coach are shown
// as short notices.
type message struct {
	Role  string // workspace.RoleUser for the learner's words, RoleNotice for app events
	Shown string
	Agent string
}

// send records the message in a thread's chat and queues it for that
// thread's coach.
func (a *App) send(w *workspace.Workspace, thread string, m message) error {
	a.post(w, workspace.ChatItem{Thread: thread, Role: m.Role, Text: m.Shown})
	s, err := a.session(w, thread)
	if err != nil {
		a.postFailure(w, thread, err.Error())
		return err
	}
	s.queue <- m.Agent
	return nil
}

// post saves a chat item and shows it in the UI.
func (a *App) post(w *workspace.Workspace, item workspace.ChatItem) {
	item.ID, item.At = uuid.New().String(), time.Now()
	_ = w.AppendChat(item)
	a.emit(w.ID, "chat", item)
}

// postFailure shows a problem in a thread as a red notice.
func (a *App) postFailure(w *workspace.Workspace, thread, msg string) {
	a.post(w, workspace.ChatItem{Thread: thread, Role: workspace.RoleNotice, Status: "failed", Text: msg})
}

// session returns a thread's live coach, starting (or resuming) it if needed,
// and marks the thread busy for the message the caller is about to queue.
// Busy starts here, not when the turn starts, so the learner sees the coach
// working while its process starts, which can take several seconds.
// The agent process starts outside a.mu, so a slow start doesn't block the
// rest of the app; concurrent callers wait for the same start.
func (a *App) session(w *workspace.Workspace, thread string) (*agentSession, error) {
	key := threadKey{w.ID, thread}
	cfg := a.Settings().coach()
	a.mu.Lock()
	s := a.sessions[key]
	// The settings changed since it started: restart it with the new ones
	// once it's idle. A busy session finishes its queue first.
	if s != nil && s.cfg != cfg && !s.busy && len(s.queue) == 0 {
		delete(a.sessions, key)
		s.close()
		s = nil
	}
	if s == nil {
		s = a.startSession(w, thread, cfg)
	}
	s.busy = true
	a.mu.Unlock()
	a.emit(w.ID, "busy", busyEvent{Thread: thread, Busy: true})
	return s, a.waitStarted(w, s)
}

// startSession starts a thread's coach process in the background. Call with
// a.mu held.
func (a *App) startSession(w *workspace.Workspace, thread string, cfg coachConfig) *agentSession {
	key := threadKey{w.ID, thread}
	ctx, cancel := context.WithCancel(context.Background())
	s := &agentSession{thread: thread, cfg: cfg, queue: make(chan string, 32), cancel: cancel, ready: make(chan struct{})}
	a.sessions[key] = s
	go a.runSession(ctx, w, s)
	if thread != workspace.PlanThread {
		a.closeIdleTaskSessions(key)
	}
	return s
}

// waitStarted waits for a session's process to start. If it couldn't, the
// thread stops showing as busy.
func (a *App) waitStarted(w *workspace.Workspace, s *agentSession) error {
	<-s.ready
	if s.startErr != nil {
		a.setBusy(w.ID, s, false)
		return s.startErr
	}
	return nil
}

// closeIdleTaskSessions stops the coach processes of the workspace's other
// idle task and lesson threads, so moving through tasks doesn't pile up
// processes. Writers close themselves when they're done.
// Their conversations resume from the saved session if reopened. Call with
// a.mu held.
func (a *App) closeIdleTaskSessions(keep threadKey) {
	for key, s := range a.sessions {
		if key.ws == keep.ws && key != keep && key.thread != workspace.PlanThread && !workspace.IsWriterThread(key.thread) && !s.busy {
			s.close()
		}
	}
}

// runSession starts the agent, then handles queued messages one turn at a
// time until the process exits or the session is closed.
func (a *App) runSession(ctx context.Context, w *workspace.Workspace, s *agentSession) {
	key := threadKey{w.ID, s.thread}
	defer func() {
		a.mu.Lock()
		if a.sessions[key] == s {
			delete(a.sessions, key)
		}
		if objective, ok := workspace.ObjectiveOfWriter(s.thread); ok {
			delete(a.checks, threadKey{w.ID, objective})
		}
		a.mu.Unlock()
		s.close()
	}()

	turn := &turnState{app: a, ws: w, thread: s.thread, tools: map[string]*workspace.ChatItem{}}
	// A writer starts fresh every time and is done after one turn.
	writer := workspace.IsWriterThread(s.thread)
	resume, err := w.UseProvider(s.cfg.Agent)
	if writer {
		resume = nil
	}
	var sess agent.Session
	if err == nil {
		sess, err = a.startAgent(ctx, s.cfg.Agent, agent.Options{
			Cwd:          w.Dir,
			MCPName:      mcpName,
			MCPURL:       a.mcp.URL(w, s.thread),
			Instructions: coachPrompt + "\n\n" + roleInstructions(w, s.thread),
			ResumeID:     resume[s.thread],
			Model:        s.cfg.Model,
			Effort:       s.cfg.Effort,
		}, turn.handle)
	}
	// Saved before ready closes, so the session id is on disk once the
	// first message is queued. Writers never resume, so theirs aren't kept.
	if err == nil && !writer {
		_ = w.SetSession(s.cfg.Agent, s.thread, sess.ID())
	}
	s.sess, s.startErr = sess, err
	close(s.ready)
	if err != nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.queue:
			a.setBusy(w.ID, s, true)
			err := sess.Prompt(ctx, msg)
			turn.flush()
			if s.stopped.Swap(false) {
				// After flush, so the notice follows the cut-off reply.
				a.post(w, workspace.ChatItem{Thread: s.thread, Role: workspace.RoleNotice, Text: "Stopped. Send a message to continue."})
			}
			if writer && a.retire(key, s) {
				if err != nil && ctx.Err() == nil {
					a.postFailure(w, s.thread, err.Error())
				}
				return
			}
			a.setBusy(w.ID, s, false)
			if err != nil && ctx.Err() == nil {
				a.postFailure(w, s.thread, err.Error())
				if errors.Is(err, agent.ErrStopped) {
					return
				}
			}
		}
	}
}

// lessonContext tells a lesson's tutor how the learner is doing on its quiz.
func lessonContext(w *workspace.Workspace, id string) string {
	d, err := w.Lesson(id)
	if err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[context]\nOpen lesson: %s (%s), objective %s. Quiz: %d of %d answered, %d correct.\n", d.ID, d.Title, d.Objective, d.Answered, d.Questions, d.Correct)
	for i, q := range d.Quiz {
		if r, ok := d.Results[i]; ok && !r.Correct {
			fmt.Fprintf(&b, "- Got question %d wrong: %q, picked %q.\n", i+1, text.Truncate(q.Question, 300), q.Options[r.Choice])
		}
	}
	b.WriteString("[/context]\n\n")
	return b.String()
}

// roleInstructions tells a coach which conversation it's in. A title that
// can't be read falls back to the id.
func roleInstructions(w *workspace.Workspace, thread string) string {
	if thread == workspace.PlanThread {
		return planRole
	}
	role, id, title := taskRole, thread, ""
	if objective, ok := workspace.ObjectiveOfWriter(thread); ok {
		role, id = writerRole, objective
		if c, err := w.Curriculum(); err == nil && c != nil {
			o, _, _ := c.Objective(id)
			title = o.Title
		}
	} else if lesson, ok := workspace.LessonOfThread(thread); ok {
		role, id = lessonRole, lesson
		d, _ := w.Lesson(id)
		title = d.Title
	} else {
		d, _ := w.Task(id)
		title = d.Title
	}
	return strings.NewReplacer("{{id}}", id, "{{title}}", cmp.Or(title, id)).Replace(role)
}

type busyEvent struct {
	Thread string `json:"thread"`
	Busy   bool   `json:"busy"`
}

// retire ends a writer after its turn, unless more work was queued for it.
// It leaves the sessions before it stops showing as busy, so a new request
// for the same objective starts a new writer instead of queueing on this one.
func (a *App) retire(key threadKey, s *agentSession) bool {
	a.mu.Lock()
	if len(s.queue) > 0 {
		a.mu.Unlock()
		return false
	}
	if a.sessions[key] == s {
		delete(a.sessions, key)
	}
	s.busy = false
	a.mu.Unlock()
	a.emit(key.ws, "busy", busyEvent{Thread: s.thread, Busy: false})
	return true
}

func (a *App) setBusy(wsID string, s *agentSession, busy bool) {
	a.mu.Lock()
	s.busy = busy
	a.mu.Unlock()
	a.emit(wsID, "busy", busyEvent{Thread: s.thread, Busy: busy})
}

// turnState turns agent events into chat items. Streaming text is sent to
// the UI as it arrives and saved to the transcript when complete.
type turnState struct {
	app    *App
	ws     *workspace.Workspace
	thread string

	mu      sync.Mutex
	current *workspace.ChatItem            // assistant message being streamed
	tools   map[string]*workspace.ChatItem // running tool calls by id; nil if hidden
}

func (t *turnState) handle(ev agent.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	a, w := t.app, t.ws
	switch ev.Kind {
	case agent.EventWarning:
		a.postFailure(w, t.thread, ev.Text)
	case agent.EventText:
		if t.current == nil {
			t.current = &workspace.ChatItem{ID: uuid.New().String(), Thread: t.thread, Role: workspace.RoleAssistant, At: time.Now()}
		}
		t.current.Text += ev.Text
		a.emit(w.ID, "chat", *t.current)
	case agent.EventTool:
		t.endText()
		tool := ev.Tool
		// A call's first event decides whether it shows; later ones only
		// update its status.
		item, seen := t.tools[tool.ID]
		if !seen {
			if label, show := toolLabel(tool); show {
				item = &workspace.ChatItem{ID: uuid.New().String(), Thread: t.thread, Role: workspace.RoleTool, At: time.Now(), Text: label}
			}
			t.tools[tool.ID] = item
		}
		if tool.Status != agent.ToolRunning {
			delete(t.tools, tool.ID)
		}
		if item == nil {
			return
		}
		item.Status = tool.Status
		a.emit(w.ID, "chat", *item)
		if tool.Status != agent.ToolRunning {
			_ = w.AppendChat(*item)
		}
	}
}

func (t *turnState) endText() {
	if t.current != nil {
		_ = t.ws.AppendChat(*t.current)
		t.current = nil
	}
}

func (t *turnState) flush() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.endText()
	for _, item := range t.tools {
		if item != nil {
			item.Status = agent.ToolDone
			_ = t.ws.AppendChat(*item)
		}
	}
	clear(t.tools)
	t.app.emit(t.ws.ID, "changed", nil)
}

// mcpName is the name the coach knows Tutortop's MCP server by.
const mcpName = "tutortop"

// toolLabel turns an agent tool call into a short line for the learner, or
// hides it. The coach reading files or keeping its todo list is noise;
// changes to the learner's mission or plan are worth seeing.
func toolLabel(t agent.Tool) (string, bool) {
	switch t.Kind {
	case agent.ToolRead, agent.ToolSearch, agent.ToolThink:
		return "", false
	case agent.ToolEdit:
		if len(t.Paths) != 1 {
			break
		}
		file := t.Paths[0]
		switch base := filepath.Base(file); {
		case base == "MISSION.md":
			return "Updated your mission", true
		case base == "NOTES.md":
			return "Noted your preferences", true
		case base == "RESOURCES.md":
			return "Updated your resources", true
		case base == "GLOSSARY.md":
			return "Updated your glossary", true
		case strings.Contains(file, "learning-records") || learningRecord.MatchString(base):
			return "Recorded what you learned", true
		}
	}
	if t.MCPServer == mcpName {
		switch t.MCPTool {
		// create_task, propose_curriculum and set_icon show in their own UI
		// (the status line, the proposal screen, the icon itself).
		case "get_state", "ask_user", "create_task", "propose_curriculum", "set_icon":
			return "", false
		}
	}
	return t.Title, t.Title != ""
}

var learningRecord = regexp.MustCompile(`^\d{4}-.*\.md$`)
