package app

import (
	"fmt"

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

// writers start objectives' writers, on the thread agents.
type writers struct {
	agents *threadAgents
}

func newWriters(agents *threadAgents) *writers {
	return &writers{agents: agents}
}

// readyAhead is how many unpassed objectives, in plan order, keepReady keeps
// written, so the learner never waits for the next one.
const readyAhead = 2

// keepReady starts writers for the next unpassed objectives in plan order
// that are missing a lesson or an unsolved exercise.
func (wr *writers) keepReady(w *workspace.Workspace, why string) {
	for _, o := range w.NeedsWriting(readyAhead) {
		wr.write(w, o, why)
	}
}

// write starts a writer for one objective, unless one is already at work on
// it. why tells the writer what prompted it. Like a chat message, it doesn't
// wait for the start; a failure is posted in the writer's thread, which the
// objective's page shows.
func (wr *writers) write(w *workspace.Workspace, o workspace.Objective, why string) {
	t := workspace.WriterThread(o.ID)
	msg := fmt.Sprintf("[app] Write for the objective “%s” (%s). %s", o.Title, o.ID, why)
	if err := wr.agents.queue(w, t, msg); err != nil {
		postFailure(w, t.String(), err.Error())
	}
}
