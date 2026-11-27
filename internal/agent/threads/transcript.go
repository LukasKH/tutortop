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
	"github.com/LukasKH/tutortop/internal/text"
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
// What the agent does in between, thinking or writing a long tool call, is
// only shown while it lasts.
type transcript struct {
	ws     *workspace.Workspace
	thread string

	mu      sync.Mutex
	closed  bool                           // agent ended: ignore late events from a dying CLI
	inTurn  bool                           // true while Prompt is running (between startTurn and endTurn)
	muted   bool                           // current turn was superseded by an answer/approval: drop trailing output until endTurn
	current *workspace.ChatItem            // assistant message being streamed
	tools   map[string]*workspace.ChatItem // running tool calls by id; nil if hidden
	working events.Working                 // shown until the next message or tool call
}

func newTranscript(w *workspace.Workspace, thread string) *transcript {
	return &transcript{
		ws:      w,
		thread:  thread,
		tools:   map[string]*workspace.ChatItem{},
		working: events.Working{Workspace: w.ID, Thread: thread},
	}
}

func (t *transcript) handle(ev agent.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.muted {
		return
	}
	w := t.ws
	switch ev.Kind {
	case agent.EventWarning:
		postFailure(w, t.thread, ev.Text)
	case agent.EventThinking:
		t.showWorking(text.Tail(t.working.Thinking+ev.Text, maxThinking), "")
	case agent.EventPreparing:
		if label := preparingLabel(ev.Tool); label != "" {
			t.showWorking(t.working.Thinking, label)
		}
	case agent.EventText:
		t.showWorking("", "")
		if t.current == nil {
			t.current = &workspace.ChatItem{ID: uuid.New().String(), Thread: t.thread, Role: workspace.RoleAssistant, At: time.Now()}
		}
		t.current.Text += ev.Text
		events.Emit(events.ChatMessage, events.Chat{Workspace: w.ID, Item: *t.current})
	case agent.EventTool:
		t.endText()
		tool := ev.Tool
		// A call's first event decides whether it shows; later ones only
		// update its status. A call that arrives already failed was never
		// shown, so there's nothing for the UI to drop.
		item, seen := t.tools[tool.ID]
		if !seen && tool.Status != agent.ToolFailed {
			if label, show := toolLabel(tool); show {
				item = &workspace.ChatItem{ID: uuid.New().String(), Thread: t.thread, Role: workspace.RoleTool, At: time.Now(), Text: label}
			}
			t.tools[tool.ID] = item
		}
		if tool.Status != agent.ToolRunning {
			delete(t.tools, tool.ID)
		}
		// Keep preparing/thinking visible while a hidden call (such as
		// propose_curriculum link checking) runs; clear once a visible call
		// takes over or any call completes.
		if item != nil || tool.Status != agent.ToolRunning {
			t.showWorking("", "")
		}
		if item == nil {
			return
		}
		item.Status = chatStatus[tool.Status]
		events.Emit(events.ChatMessage, events.Chat{Workspace: w.ID, Item: *item})
		// A failed call is the tutor's to recover from, not the learner's:
		// the UI drops it, and it isn't saved.
		if tool.Status == agent.ToolDone {
			_ = w.AppendChat(*item)
		}
	}
}

// close saves any streamed reply and ends the transcript for good, clearing
// live working state so a dying CLI can't emit stale updates after its agent
// was stopped or retried.
func (t *transcript) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	t.endText()
	t.showWorking("", "")
}

// chatStatus is how a tool call's status shows in the chat.
var chatStatus = map[agent.ToolStatus]workspace.ChatStatus{
	agent.ToolRunning: workspace.ChatRunning,
	agent.ToolDone:    workspace.ChatDone,
	agent.ToolFailed:  workspace.ChatFailed,
}

// showWorking shows what the agent is doing between messages and tool
// calls, if it changed; empty strings clear it.
func (t *transcript) showWorking(thinking, preparing string) {
	if thinking == t.working.Thinking && preparing == t.working.Preparing {
		return
	}
	t.working.Thinking, t.working.Preparing = thinking, preparing
	events.Emit(events.ThreadWorking, t.working)
}

func (t *transcript) endText() {
	if t.current != nil {
		_ = t.ws.AppendChat(*t.current)
		t.current = nil
	}
}

// startTurn marks the start of a new agent turn so its events stream normally.
func (t *transcript) startTurn() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inTurn = true
	t.muted = false
}

// supersedeTurn flushes any already-streamed output from an in-flight turn and
// mutes the rest of that turn until endTurn. Used when the learner answers
// ask_user or approves/rejects a proposal before the tutor's closing prose ends.
func (t *transcript) supersedeTurn() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || !t.inTurn {
		return
	}
	t.endText()
	t.showWorking("", "")
	for _, item := range t.tools {
		if item != nil {
			item.Status = workspace.ChatDone
			_ = t.ws.AppendChat(*item)
			events.Emit(events.ChatMessage, events.Chat{Workspace: t.ws.ID, Item: *item})
		}
	}
	clear(t.tools)
	t.muted = true
}

// endTurn saves what's still open once a turn ends, and has the UI reload
// the workspace the agent may have changed.
func (t *transcript) endTurn() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inTurn = false
	muted := t.muted
	t.muted = false
	if t.closed {
		return
	}
	if !muted {
		t.endText()
		t.showWorking("", "")
		for _, item := range t.tools {
			if item != nil {
				item.Status = workspace.ChatDone
				_ = t.ws.AppendChat(*item)
			}
		}
		clear(t.tools)
	}
	events.Emit(events.WorkspaceChanged, events.Workspace{Workspace: t.ws.ID})
}

// toolLabel turns an agent tool call into a short line for the learner, or
// hides it. The tutor reading files or keeping its todo list is noise, and
// the policy refuses every shell command, so those only ever fail; changes to
// the learner's mission or plan are worth seeing. Our own tools show as their
// entry in the tool table says (mcp.ChatLine).
func toolLabel(t agent.Tool) (string, bool) {
	if t.MCPServer == mcp.Name {
		return mcp.ChatLine(t.MCPTool)
	}
	switch t.Kind {
	case agent.ToolRead, agent.ToolSearch, agent.ToolThink, agent.ToolExecute:
		return "", false
	case agent.ToolEdit:
		if label := editLabel(t.Paths); label != "" {
			return label, true
		}
	case agent.ToolFetch, agent.ToolOther:
		// They show as the agent titles them, below.
	}
	return t.Title, t.Title != ""
}

// preparingLabel is the line shown while the agent writes a tool call, or ""
// for a call that's quick to write. Only its name is known yet: an edit's
// file isn't.
func preparingLabel(t agent.Tool) string {
	if t.MCPServer == mcp.Name {
		return mcp.PreparingLine(t.MCPTool)
	}
	if t.Kind == agent.ToolEdit {
		return "Writing a file"
	}
	return ""
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

// maxThinking is how much of the agent's latest reasoning the UI gets, in
// bytes: enough to read, while long reasoning doesn't make every update
// heavier.
const maxThinking = 4000
