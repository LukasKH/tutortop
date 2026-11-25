// Package events holds the events the backend pushes to the frontend: each
// one's name, the data it carries, and Emit, which sends it. Any package may
// emit them: a service, a thread's agent or an MCP tool.
package events

import (
	"sync/atomic"

	"github.com/LukasKH/tutortop/internal/workspace"
)

// Name is the name of an event. Its type says what data the event carries,
// so Emit with the wrong data doesn't compile.
type Name[Data any] string

const (
	ChatMessage        Name[Chat]      = "chat:message"
	ThreadBusy         Name[Busy]      = "thread:busy"
	ThreadWorking      Name[Working]   = "thread:working"
	CheckStatus        Name[Check]     = "check:status"
	TaskPublished      Name[Task]      = "task:published"
	LessonPublished    Name[Lesson]    = "lesson:published"
	QuestionsAsked     Name[Questions] = "questions:asked"
	LSPMessage         Name[LSP]       = "lsp:message"
	WorkspaceChanged   Name[Workspace] = "workspace:changed" // the frontend reloads the workspace
	CurriculumProposed Name[Workspace] = "curriculum:proposed"
	CopilotInstalled   Name[Install]   = "copilot:installed" // the Settings screen checks Copilot again
)

// emitter is wired by main.go to Wails' event bus. Tests leave it unset.
var emitter atomic.Pointer[func(name string, data any)]

// SetEmitter sets where Emit sends events. main.go points it at Wails;
// tests leave it unset and Emit does nothing.
func SetEmitter(emit func(name string, data any)) {
	if emit == nil {
		emitter.Store(nil)
		return
	}
	emitter.Store(&emit)
}

// Emit sends an event to the frontend. Tests run without a Wails app, and
// there it does nothing.
func Emit[Data any](name Name[Data], data Data) {
	if emit := emitter.Load(); emit != nil {
		(*emit)(string(name), data)
	}
}

// Workspace names the workspace that changed.
type Workspace struct {
	Workspace string `json:"workspace"`
}

// Install tells that installing Copilot's language server has ended.
type Install struct {
	Error string `json:"error,omitempty"` // why it failed; "" if it worked
}

// Chat is a new or updated chat message.
type Chat struct {
	Workspace string             `json:"workspace"`
	Item      workspace.ChatItem `json:"item"`
}

// Busy tells when a thread's tutor or writer starts or stops working.
type Busy struct {
	Workspace string `json:"workspace"`
	Thread    string `json:"thread"`
	Busy      bool   `json:"busy"`
	Seq       uint64 `json:"seq"` // orders it among snapshots (app.WorkspaceSnapshot.Seq)
}

// Working is what a thread's agent is doing between its messages and tool
// calls: its reasoning, or a long tool call it's writing. It's shown live and
// never saved, and sent again empty once a message or tool call takes over,
// or the turn ends.
type Working struct {
	Workspace string `json:"workspace"`
	Thread    string `json:"thread"`
	// Thinking is the end of the agent's reasoning since its last message or
	// tool call, as far as its CLI shows it.
	Thinking string `json:"thinking"`
	// Preparing is the tool call it's writing, for the learner: "Drafting
	// your plan".
	Preparing string `json:"preparing"`
}

// Check tells how a sandbox check is going.
type Check struct {
	Workspace string       `json:"workspace"`
	Check     SandboxCheck `json:"check"`
	Seq       uint64       `json:"seq"` // orders it among snapshots (app.WorkspaceSnapshot.Seq)
}

// SandboxCheck is a check in the sandbox that an agent waits for and the
// learner watches: a writer's new task in the quality gate, or a language a
// chat sets up with set_toolchain. app.WorkspaceSnapshot.Checks keeps
// writers' latest ones.
type SandboxCheck struct {
	Kind CheckKind `json:"kind"`
	// Thread is the thread whose agent waits for it: an objective's writer
	// for a task, the chat that called set_toolchain for a toolchain.
	Thread  string `json:"thread"`
	Title   string `json:"title"`
	Running bool   `json:"running"`
	Passed  bool   `json:"passed"`
}

// CheckKind is what a SandboxCheck checks.
type CheckKind string

const (
	CheckTask      CheckKind = "task"      // a writer's new task, in the quality gate
	CheckToolchain CheckKind = "toolchain" // a language a chat sets up with set_toolchain
)

// Task announces a newly published task.
type Task struct {
	Workspace string             `json:"workspace"`
	Task      workspace.TaskInfo `json:"task"`
}

// Lesson announces a newly published lesson.
type Lesson struct {
	Workspace string               `json:"workspace"`
	Lesson    workspace.LessonInfo `json:"lesson"`
}

// Questions carries the tutor's questions for the learner.
type Questions struct {
	Workspace string              `json:"workspace"`
	Questions workspace.Questions `json:"questions"`
}

// LSP carries one message from a language server to the editor, or tells it
// the server has stopped.
type LSP struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Closed  bool   `json:"closed"`
}
