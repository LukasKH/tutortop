package app

import (
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type WorkspaceEvent struct {
	Workspace string `json:"workspace"`
}

// ChatEvent is a new or updated chat message.
type ChatEvent struct {
	Workspace string             `json:"workspace"`
	Item      workspace.ChatItem `json:"item"`
}

// BusyEvent tells when a thread's tutor or writer starts or stops working.
type BusyEvent struct {
	Workspace string `json:"workspace"`
	Thread    string `json:"thread"`
	Busy      bool   `json:"busy"`
	Seq       uint64 `json:"seq"` // orders it among snapshots (WorkspaceSnapshot.Seq)
}

// CheckEvent tells how a sandbox check is going.
type CheckEvent struct {
	Workspace string       `json:"workspace"`
	Check     SandboxCheck `json:"check"`
	Seq       uint64       `json:"seq"` // orders it among snapshots (WorkspaceSnapshot.Seq)
}

// SandboxCheck is a check in the sandbox that an agent waits for and the
// learner watches: a writer's new task in the quality gate, or a language a
// chat sets up with set_toolchain. WorkspaceSnapshot.Checks keeps writers'
// latest ones.
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

// TaskEvent announces a newly published task.
type TaskEvent struct {
	Workspace string             `json:"workspace"`
	Task      workspace.TaskInfo `json:"task"`
}

// LessonEvent announces a newly published lesson.
type LessonEvent struct {
	Workspace string               `json:"workspace"`
	Lesson    workspace.LessonInfo `json:"lesson"`
}

// QuestionsEvent carries the tutor's questions for the learner.
type QuestionsEvent struct {
	Workspace string              `json:"workspace"`
	Questions workspace.Questions `json:"questions"`
}

// LSPEvent carries one message from a language server to the editor, or
// tells it the server has stopped.
type LSPEvent struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Closed  bool   `json:"closed"`
}

// event is the name of an event the backend pushes to the frontend. Its type
// says what data the event carries, so emit with the wrong data doesn't compile.
type event[Data any] string

const (
	chatMessage        event[ChatEvent]      = "chat:message"
	threadBusy         event[BusyEvent]      = "thread:busy"
	checkStatus        event[CheckEvent]     = "check:status"
	taskPublished      event[TaskEvent]      = "task:published"
	lessonPublished    event[LessonEvent]    = "lesson:published"
	questionsAsked     event[QuestionsEvent] = "questions:asked"
	lspMessage         event[LSPEvent]       = "lsp:message"
	workspaceChanged   event[WorkspaceEvent] = "workspace:changed" // the frontend reloads the workspace
	curriculumProposed event[WorkspaceEvent] = "curriculum:proposed"
)

// Each event is registered with its data type, so the generated bindings type
// it in TypeScript and Wails rejects an emit with the wrong type.
func init() {
	application.RegisterEvent[ChatEvent](string(chatMessage))
	application.RegisterEvent[BusyEvent](string(threadBusy))
	application.RegisterEvent[CheckEvent](string(checkStatus))
	application.RegisterEvent[TaskEvent](string(taskPublished))
	application.RegisterEvent[LessonEvent](string(lessonPublished))
	application.RegisterEvent[QuestionsEvent](string(questionsAsked))
	application.RegisterEvent[LSPEvent](string(lspMessage))
	application.RegisterEvent[WorkspaceEvent](string(workspaceChanged))
	application.RegisterEvent[WorkspaceEvent](string(curriculumProposed))
}

// emit sends an event to the frontend. Tests run without a Wails app, and
// there it does nothing.
func emit[Data any](name event[Data], data Data) {
	if wails := application.Get(); wails != nil {
		wails.Event.Emit(string(name), data)
	}
}
