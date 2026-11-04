package app

import (
	"errors"
	"fmt"

	"github.com/LukasKH/tutortop/internal/workspace"
)

// Writers prepare lessons and exercises in the background. Each objective
// gets its own short-lived agent conversation, so several objectives are
// written at once, and the tutoring and planning chats stay free for the
// learner. A writer isn't shown as a chat; the UI shows its objective as
// being written while it's busy.
//
// A writer runs in a session like any thread (session.go), with three
// differences: it starts a fresh conversation, it ends once it has nothing
// left to do, and its MCP tools publish lessons and tasks (mcp.go).

// PrepareObjective has a writer write an objective's lesson and task now,
// because the learner opened it ahead of the suggested order. It returns
// once the writer is asked; the lesson and task arrive as events, and a
// writer that fails posts why in its thread.
func (a *App) PrepareObjective(wsID, objectiveID string) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	c, err := w.Curriculum()
	if err != nil {
		return err
	}
	if c == nil {
		return errors.New("there's no plan yet")
	}
	o, _, ok := c.Objective(objectiveID)
	if !ok {
		return fmt.Errorf("%q isn't in the plan", objectiveID)
	}
	a.writeObjective(w, o, "The learner opened it and is waiting for it, so be quick. Write the lesson first, as they read it first.")
	return nil
}

// Gate tells the UI a new task, or a new language's sandbox (Kind
// "toolchain"), is being checked.
type Gate struct {
	Kind      string `json:"kind,omitempty"`
	Objective string `json:"objective,omitempty"` // for a task: whose writer is checking it
	Title     string `json:"title"`
	Running   bool   `json:"running"`
	Passed    bool   `json:"passed"`
}

// readyAhead is how many unpassed objectives, in plan order, keepReady keeps
// written, so the learner never waits for the next one.
const readyAhead = 2

// writeObjective starts a writer for one objective, unless one is already at
// work on it. why tells the writer what prompted it. Like a chat message, it
// doesn't wait for the start; a failure is posted in the writer's thread,
// which the objective's page shows.
func (a *App) writeObjective(w *workspace.Workspace, o workspace.Objective, why string) {
	t := workspace.WriterThread(o.ID)
	msg := fmt.Sprintf("[app] Write for the objective “%s” (%s). %s", o.Title, o.ID, why)
	if err := a.enqueue(w, t, msg, true); err != nil {
		a.postFailure(w, t.String(), err.Error())
	}
}

// writerCheck records and shows how a writer's sandbox check of its task is
// going, so the objective's page shows it even after the learner looks away.
// The check is kept on the writer's session, so it goes when the writer does.
func (a *App) writerCheck(wsID string, g Gate) {
	a.mu.Lock()
	if s := a.sessions[threadKey{wsID, workspace.WriterThread(g.Objective).String()}]; s != nil {
		s.gate = &g
	}
	a.mu.Unlock()
	emit(gateStatus, GateEvent{Workspace: wsID, Gate: g})
}

// keepReady starts writers for the next unpassed objectives in plan order
// that are missing a lesson or an unsolved exercise.
func (a *App) keepReady(w *workspace.Workspace, why string) {
	for _, o := range w.NeedsWriting(readyAhead) {
		a.writeObjective(w, o, why)
	}
}
