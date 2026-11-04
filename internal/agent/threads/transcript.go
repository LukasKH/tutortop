package threads

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/mcp"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Post saves a chat item and shows it in the UI.
func Post(w *workspace.Workspace, item workspace.ChatItem) {
	item.ID, item.At = uuid.New().String(), time.Now()
	_ = w.AppendChat(item)
	events.Emit(events.ChatMessage, events.Chat{Workspace: w.ID, Item: item})
}

// postFailure shows a problem in a thread as a red notice.
func postFailure(w *workspace.Workspace, thread, msg string) {
	Post(w, workspace.ChatItem{Thread: thread, Role: workspace.RoleNotice, Status: workspace.ChatFailed, Text: msg})
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
