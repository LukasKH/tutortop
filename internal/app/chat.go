package app

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// The chat: what the learner sends to a thread's agent, and how its replies
// show up. send posts a message to the thread's chat and queues it for the
// thread's session (session.go); the session's transcript streams the reply
// into the chat as it arrives.

// Chat returns one thread's chat.
func (a *App) Chat(wsID, thread string) ([]workspace.ChatItem, error) {
	w, err := a.store.Open(wsID)
	if err != nil {
		return nil, err
	}
	return w.Chat(thread)
}

// SendMessage sends the learner's chat message to a thread, by its name
// ("plan", a task id, ...). In a task's or lesson's thread the app adds a
// [context] block about it.
func (a *App) SendMessage(wsID, text, thread string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	t, err := workspace.ParseThread(thread)
	if err != nil {
		return err
	}
	return a.send(w, t, message{Role: workspace.RoleUser, Shown: text, Agent: a.contextFor(w, t) + text})
}

// AnswerQuestions sends the learner's answers to the thread that asked.
func (a *App) AnswerQuestions(wsID string, answers map[string]string) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	t := workspace.PlanThread
	var b strings.Builder
	b.WriteString("My answers:\n")
	if q := w.PendingQuestions(); q != nil {
		if t, err = workspace.ParseThread(q.Thread); err != nil {
			return err
		}
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
	return a.send(w, t, message{Role: workspace.RoleUser, Shown: b.String(), Agent: b.String()})
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
	t := workspace.TaskThread(taskID)
	return a.send(w, t, message{Role: workspace.RoleUser, Shown: msg, Agent: a.contextFor(w, t) + msg})
}

// StopAgent interrupts the current turn in a thread. The session posts a
// notice once the cut-off reply is saved.
func (a *App) StopAgent(wsID, thread string) {
	a.mu.Lock()
	s := a.sessions[threadKey{wsID, thread}]
	a.mu.Unlock()
	if s == nil || !s.started() {
		return
	}
	s.stop.Store(true)
	s.agent.Cancel()
}

// message is one thing sent to the tutor. What the learner sees in the chat
// (Shown) can differ from what the tutor receives (Agent): the app attaches
// the open task as context, and its own instructions to the tutor are shown
// as short notices.
type message struct {
	Role  workspace.ChatRole // workspace.RoleUser for the learner's words, RoleNotice for app events
	Shown string
	Agent string
}

// send records the message in a thread's chat and queues it for that
// thread's agent.
func (a *App) send(w *workspace.Workspace, t workspace.Thread, m message) error {
	a.post(w, workspace.ChatItem{Thread: t.String(), Role: m.Role, Text: m.Shown})
	if err := a.enqueue(w, t, m.Agent, false); err != nil {
		a.postFailure(w, t.String(), err.Error())
		return err
	}
	return nil
}

// post saves a chat item and shows it in the UI.
func (a *App) post(w *workspace.Workspace, item workspace.ChatItem) {
	item.ID, item.At = uuid.New().String(), time.Now()
	_ = w.AppendChat(item)
	emit(chatMessage, ChatEvent{Workspace: w.ID, Item: item})
}

// postFailure shows a problem in a thread as a red notice.
func (a *App) postFailure(w *workspace.Workspace, thread, msg string) {
	a.post(w, workspace.ChatItem{Thread: thread, Role: workspace.RoleNotice, Status: "failed", Text: msg})
}

// transcript turns one session's agent events into chat items. Streaming
// text is sent to the UI as it arrives and saved to the chat when complete.
type transcript struct {
	app    *App
	ws     *workspace.Workspace
	thread string

	mu      sync.Mutex
	current *workspace.ChatItem            // assistant message being streamed
	tools   map[string]*workspace.ChatItem // running tool calls by id; nil if hidden
}

func (t *transcript) handle(ev agent.Event) {
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
		emit(chatMessage, ChatEvent{Workspace: w.ID, Item: *t.current})
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
		item.Status = string(tool.Status)
		emit(chatMessage, ChatEvent{Workspace: w.ID, Item: *item})
		if tool.Status != agent.ToolRunning {
			_ = w.AppendChat(*item)
		}
	}
}

func (t *transcript) endText() {
	if t.current != nil {
		_ = t.ws.AppendChat(*t.current)
		t.current = nil
	}
}

// endTurn saves what's still open once a turn ends, and has the UI reload
// the workspace the agent may have changed.
func (t *transcript) endTurn() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.endText()
	for _, item := range t.tools {
		if item != nil {
			item.Status = string(agent.ToolDone)
			_ = t.ws.AppendChat(*item)
		}
	}
	clear(t.tools)
	emit(workspaceChanged, WorkspaceEvent{Workspace: t.ws.ID})
}

// mcpName is the name the tutor knows Tutortop's MCP server by.
const mcpName = "tutortop"

// toolLabel turns an agent tool call into a short line for the learner, or
// hides it. The tutor reading files or keeping its todo list is noise;
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
