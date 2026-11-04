package workspace

import (
	"fmt"
	"os"
	"time"
)

// ChatRole is who or what a chat item comes from.
type ChatRole string

const (
	RoleUser      ChatRole = "user"      // the learner's words
	RoleAssistant ChatRole = "assistant" // the tutor's reply
	RoleTool      ChatRole = "tool"      // a tutor action worth showing, e.g. "Updated your mission"
	RoleNotice    ChatRole = "notice"    // an app event, e.g. "Stopped" or "You passed …"
)

// ChatItem is one entry in the conversation shown in the UI.
type ChatItem struct {
	ID     string     `json:"id"`
	Thread string     `json:"thread"`
	Role   ChatRole   `json:"role"`
	Text   string     `json:"text"`
	Status ChatStatus `json:"status,omitempty"` // tools always have one; notices only ChatFailed
	Mark   ChatMark   `json:"mark,omitempty"`
	At     time.Time  `json:"at"`
}

// ChatMark tells what an item is, for code that looks for it, so nothing
// matches on the text the learner reads.
type ChatMark string

const (
	// MarkPlanApproved is the notice that the learner approved a plan. The
	// chat panel hides the plan thread up to the first one (afterSetup in
	// frontend/src/lib/chat.ts). Notices saved before marks existed have
	// none, and it finds them by their text.
	MarkPlanApproved ChatMark = "plan-approved"
)

// ChatStatus is how a tool's call went, or marks a notice as a failure.
type ChatStatus string

const (
	ChatRunning ChatStatus = "running" // a tool call still at work
	ChatDone    ChatStatus = "done"
	ChatFailed  ChatStatus = "failed" // a tool call that failed, or a notice about a problem
)

func (w *Workspace) AppendChat(item ChatItem) error {
	if !validName(item.Thread) {
		return fmt.Errorf("invalid chat thread %q", item.Thread)
	}
	// A tutor still winding down after its workspace was deleted mustn't
	// bring the folder back.
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := os.Stat(w.path(".tutortop", "workspace.json")); err != nil {
		return err
	}
	if err := os.MkdirAll(w.path(".tutortop", "chat"), 0o755); err != nil {
		return err
	}
	return appendJSONL(w.path(".tutortop", "chat", item.Thread+".jsonl"), item)
}

// Chat returns one thread's transcript.
func (w *Workspace) Chat(thread string) ([]ChatItem, error) {
	if !validName(thread) {
		return nil, fmt.Errorf("invalid chat thread %q", thread)
	}
	items, err := readJSONL[ChatItem](w.path(".tutortop", "chat", thread+".jsonl"))
	// Every item in the file is the thread's. Older files may leave Thread
	// empty for the plan.
	for i := range items {
		items[i].Thread = thread
	}
	return items, err
}

type Question struct {
	ID          string   `json:"id" jsonschema:"short key for the answer"`
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty" jsonschema:"2-4 suggested answers; the learner can always write their own"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
}

// Questions is a form from the tutor that waits for the learner's answers.
type Questions struct {
	Thread    string     `json:"thread"` // the thread that asked, which gets the answers
	Intro     string     `json:"intro,omitempty"`
	Questions []Question `json:"questions"`
}

func (w *Workspace) SetQuestions(q *Questions) error {
	p := w.path(".tutortop", "questions.json")
	if q == nil {
		return removeIfExists(p)
	}
	return writeJSON(p, q)
}

// PendingQuestions returns the unanswered form, or nil.
func (w *Workspace) PendingQuestions() *Questions {
	q, err := readJSON[Questions](w.path(".tutortop", "questions.json"))
	if err != nil {
		return nil
	}
	// Older forms left Thread out for the plan.
	if q.Thread == "" {
		q.Thread = PlanThread.String()
	}
	return &q
}
