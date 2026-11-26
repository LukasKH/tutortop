package threads

import (
	"fmt"

	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Writers write lessons and exercises in the background. Each objective
// gets its own short-lived agent conversation, so several objectives are
// written at once, and the tutoring and planning chats stay free for the
// learner. A writer isn't shown as a chat; the UI shows its objective as
// being written while it's busy.
//
// A writer runs as a threadAgent like any thread (agents.go), with four
// differences: it starts a fresh conversation, it takes no second message
// while it's busy, it ends once it has nothing left to do, and its MCP tools
// publish lessons and tasks (internal/mcp).

// Writers start objectives' writers, on the thread agents.
type Writers struct {
	agents *Agents
}

// NewWriters starts writers on the thread agents.
func NewWriters(agents *Agents) *Writers {
	return &Writers{agents: agents}
}

// readyAhead is how many unpassed objectives, in plan order, KeepReady keeps
// written, so the learner never waits for the next one.
const readyAhead = 2

// KeepReady starts writers for the next unpassed objectives in plan order
// that are missing a lesson or an unsolved exercise.
func (wr *Writers) KeepReady(w *workspace.Workspace, why string) {
	for _, o := range w.NeedsWriting(readyAhead) {
		wr.Write(w, o, why)
	}
}

// Rewrite stops any running writers for w, clears already-written lessons
// (except keepLessonID) and tasks of unpassed objectives, and starts fresh
// writers for them (plus KeepReady for any missing ones ahead).
func (wr *Writers) Rewrite(w *workspace.Workspace, keepLessonID, why string) error {
	wr.agents.endWriters(w.ID)
	cleared, err := w.ClearUnsolved(keepLessonID)
	if err != nil {
		return err
	}
	if len(cleared) > 0 {
		events.Emit(events.WorkspaceChanged, events.Workspace{Workspace: w.ID})
	}
	started := map[string]bool{}
	for _, o := range cleared {
		started[o.ID] = true
		wr.Write(w, o, why)
	}
	for _, o := range w.NeedsWriting(readyAhead) {
		if !started[o.ID] {
			wr.Write(w, o, why)
		}
	}
	return nil
}

// Write starts a writer for one objective, unless one is already at work on
// it. why tells the writer what prompted it, and the message carries the
// objective's sources (writerContext). Like a chat message, it doesn't wait
// for the start; a failure is posted in the writer's thread, which the
// objective's page shows.
func (wr *Writers) Write(w *workspace.Workspace, o workspace.Objective, why string) {
	t := workspace.WriterThread(o.ID)
	msg := contextFor(w, t) + fmt.Sprintf("[app] Write for the objective “%s” (%s). %s", o.Title, o.ID, why)
	if err := wr.agents.queue(w, t, prompt{text: msg}); err != nil {
		postFailure(w, t.String(), err.Error())
	}
}
