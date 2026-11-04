package app

import (
	"fmt"

	"github.com/LukasKH/tutortop/internal/workspace"
)

// Writers prepare lessons and exercises in the background. Each objective
// gets its own short-lived agent conversation, so several objectives are
// written at once, and the tutoring and planning chats stay free for the
// learner. A writer isn't shown as a chat; the UI shows its objective as
// being written while it's busy.

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
// work on it. why tells the writer what prompted it.
func (a *App) writeObjective(w *workspace.Workspace, o workspace.Objective, why string) error {
	thread := workspace.WriterThread(o.ID)
	cfg := a.Settings().tutor()
	a.mu.Lock()
	if a.sessions[threadKey{w.ID, thread}] != nil {
		a.mu.Unlock()
		return nil
	}
	s := a.startSession(w, thread, cfg)
	s.busy = true
	a.mu.Unlock()
	emit(threadBusy, BusyEvent{Workspace: w.ID, Thread: thread, Busy: true})

	if err := a.waitStarted(w, s); err != nil {
		a.postFailure(w, thread, err.Error())
		return err
	}
	s.queue <- fmt.Sprintf("[app] Write for the objective “%s” (%s). %s", o.Title, o.ID, why)
	return nil
}

// writerCheck records and shows how a writer's sandbox check of its task is
// going, so the objective's page shows it even after the learner looks away.
func (a *App) writerCheck(wsID string, g Gate) {
	a.mu.Lock()
	a.checks[threadKey{wsID, g.Objective}] = g
	a.mu.Unlock()
	emit(gateStatus, GateEvent{Workspace: wsID, Gate: g})
}

// keepReady starts writers for the next unpassed objectives in plan order
// that are missing a lesson or an unsolved exercise.
func (a *App) keepReady(w *workspace.Workspace, why string) {
	for _, o := range w.NeedsWriting(readyAhead) {
		go a.writeObjective(w, o, why)
	}
}
