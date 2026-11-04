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
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/mcp"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ChatService serves the chats: what the learner sends to a thread's agent,
// which answers in the thread's chat as chat:message events. It owns the
// thread agents' lifetime: it ends them all as the app quits.
//
// threadAgents.send posts a message in the thread's chat and queues it for
// the thread's agent (agents.go); a transcript (below) streams the reply into
// the chat as it arrives.
type ChatService struct {
	store  *workspace.Store
	agents *threadAgents
}

var _ application.ServiceShutdown = (*ChatService)(nil)

func newChatService(store *workspace.Store, agents *threadAgents) *ChatService {
	return &ChatService{store: store, agents: agents}
}

// ServiceShutdown is called by Wails on exit and ends every thread's agent,
// writers included.
func (s *ChatService) ServiceShutdown() error {
	s.agents.endAll()
	return nil
}

// Chat returns one thread's chat.
func (s *ChatService) Chat(wsID, thread string) ([]workspace.ChatItem, error) {
	w, err := s.store.Open(wsID)
	if err != nil {
		return nil, err
	}
	return w.Chat(thread)
}

// SendMessage sends the learner's chat message to a thread, by its name
// ("plan", a task id, ...). In a task's or lesson's thread the app adds a
// [context] block about it. It returns once the message is queued; the
// reply, or why the agent couldn't answer, arrives in the chat. An error
// means the message wasn't sent at all.
func (s *ChatService) SendMessage(wsID, text, thread string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	t, err := workspace.ParseThread(thread)
	if err != nil {
		return err
	}
	s.agents.send(w, t, message{Role: workspace.RoleUser, Shown: text, Agent: contextFor(w, t) + text})
	return nil
}

// AnswerQuestions sends the learner's answers to the thread that asked.
func (s *ChatService) AnswerQuestions(wsID string, answers map[string]string) error {
	w, err := s.store.Open(wsID)
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
	s.agents.send(w, t, message{Role: workspace.RoleUser, Shown: b.String(), Agent: b.String()})
	return nil
}

// DismissQuestions drops the pending form without answering.
func (s *ChatService) DismissQuestions(wsID string) error {
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	return w.SetQuestions(nil)
}

// RequestHint records the hint (it counts against mastery) and asks the
// task's tutor for it.
func (s *ChatService) RequestHint(wsID, taskID string, level int) error {
	if level < 1 || level > 4 {
		return errors.New("hint level must be 1-4")
	}
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	_ = w.RecordAttempt(workspace.Attempt{TaskID: taskID, Kind: workspace.AttemptHint, HintLevel: level})
	names := []string{"", "a nudge", "a pointer", "a snippet", "the solution walkthrough"}
	msg := fmt.Sprintf("Hint level %d (%s), please.", level, names[level])
	t := workspace.TaskThread(taskID)
	s.agents.send(w, t, message{Role: workspace.RoleUser, Shown: msg, Agent: contextFor(w, t) + msg})
	return nil
}

// StopAgent interrupts the current turn in a thread. Its agent posts a
// notice once the cut-off reply is saved.
func (s *ChatService) StopAgent(wsID, thread string) { s.agents.stop(wsID, thread) }

// post saves a chat item and shows it in the UI.
func post(w *workspace.Workspace, item workspace.ChatItem) {
	item.ID, item.At = uuid.New().String(), time.Now()
	_ = w.AppendChat(item)
	events.Emit(events.ChatMessage, events.Chat{Workspace: w.ID, Item: item})
}

// postFailure shows a problem in a thread as a red notice.
func postFailure(w *workspace.Workspace, thread, msg string) {
	post(w, workspace.ChatItem{Thread: thread, Role: workspace.RoleNotice, Status: workspace.ChatFailed, Text: msg})
}

// transcript turns one thread agent's events into chat items. Streaming
// text is sent to the UI as it arrives and saved to the chat when complete.
type transcript struct {
	ws     *workspace.Workspace
	thread string

	mu      sync.Mutex
	current *workspace.ChatItem            // assistant message being streamed
	tools   map[string]*workspace.ChatItem // running tool calls by id; nil if hidden
}

func newTranscript(w *workspace.Workspace, thread string) *transcript {
	return &transcript{ws: w, thread: thread, tools: map[string]*workspace.ChatItem{}}
}

func (t *transcript) handle(ev agent.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	w := t.ws
	switch ev.Kind {
	case agent.EventWarning:
		postFailure(w, t.thread, ev.Text)
	case agent.EventText:
		if t.current == nil {
			t.current = &workspace.ChatItem{ID: uuid.New().String(), Thread: t.thread, Role: workspace.RoleAssistant, At: time.Now()}
		}
		t.current.Text += ev.Text
		events.Emit(events.ChatMessage, events.Chat{Workspace: w.ID, Item: *t.current})
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
		item.Status = chatStatus[tool.Status]
		events.Emit(events.ChatMessage, events.Chat{Workspace: w.ID, Item: *item})
		if tool.Status != agent.ToolRunning {
			_ = w.AppendChat(*item)
		}
	}
}

// chatStatus is how a tool call's status shows in the chat.
var chatStatus = map[agent.ToolStatus]workspace.ChatStatus{
	agent.ToolRunning: workspace.ChatRunning,
	agent.ToolDone:    workspace.ChatDone,
	agent.ToolFailed:  workspace.ChatFailed,
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
			item.Status = workspace.ChatDone
			_ = t.ws.AppendChat(*item)
		}
	}
	clear(t.tools)
	events.Emit(events.WorkspaceChanged, events.Workspace{Workspace: t.ws.ID})
}

// toolLabel turns an agent tool call into a short line for the learner, or
// hides it. The tutor reading files or keeping its todo list is noise;
// changes to the learner's mission or plan are worth seeing. Our own tools
// show as their entry in the tool table says (mcp.ChatLine).
func toolLabel(t agent.Tool) (string, bool) {
	if t.MCPServer == mcp.Name {
		return mcp.ChatLine(t.MCPTool)
	}
	switch t.Kind {
	case agent.ToolRead, agent.ToolSearch, agent.ToolThink:
		return "", false
	case agent.ToolEdit:
		if label := editLabel(t.Paths); label != "" {
			return label, true
		}
	case agent.ToolExecute, agent.ToolFetch, agent.ToolOther:
		// They show as the agent titles them, below.
	}
	return t.Title, t.Title != ""
}

// editLabel is the line for an edit of one of the learner's own documents,
// or "" for any other edit.
func editLabel(paths []string) string {
	if len(paths) != 1 {
		return ""
	}
	file := paths[0]
	switch base := filepath.Base(file); {
	case base == "MISSION.md":
		return "Updated your mission"
	case base == "NOTES.md":
		return "Noted your preferences"
	case base == "RESOURCES.md":
		return "Updated your resources"
	case base == "GLOSSARY.md":
		return "Updated your glossary"
	case strings.Contains(file, "learning-records") || learningRecord.MatchString(base):
		return "Recorded what you learned"
	}
	return ""
}

var learningRecord = regexp.MustCompile(`^\d{4}-.*\.md$`)
