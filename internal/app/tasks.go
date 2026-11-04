package app

import (
	"context"
	"fmt"

	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/workspace"
)

func (a *App) Task(wsID, taskID string) (workspace.TaskDetail, error) {
	w, err := a.store.Open(wsID)
	if err != nil {
		return workspace.TaskDetail{}, err
	}
	return w.Task(taskID)
}

func (a *App) SaveWork(wsID, taskID string, files runner.Files) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	return w.SaveWork(taskID, files)
}

func (a *App) ResetWork(wsID, taskID string) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	return w.ResetWork(taskID)
}

// RunChecks runs the visible tests, or all tests when submit is true.
// A passing submission tells the planning tutor, and writers prepare what
// comes next.
func (a *App) RunChecks(wsID, taskID string, files runner.Files, submit bool) (runner.Result, error) {
	w, err := a.store.Open(wsID)
	if err != nil {
		return runner.Result{}, err
	}
	if err := w.SaveWork(taskID, files); err != nil {
		return runner.Result{}, err
	}
	job, err := w.Job(taskID, submit)
	if err != nil {
		return runner.Result{}, err
	}
	res, err := a.runner.Run(context.Background(), job)
	if err != nil {
		return res, err
	}
	a.mu.Lock()
	a.lastResults[taskKey{wsID, taskID}] = res
	a.mu.Unlock()
	kind := workspace.KindRun
	if submit {
		kind = workspace.KindSubmit
	}
	p, t := res.Counts()
	firstPass := submit && res.Status == runner.StatusPassed && !w.TaskPassed(taskID)
	_ = w.RecordAttempt(workspace.Attempt{TaskID: taskID, Kind: kind, Status: res.Status, Passed: p, Total: t})
	emit(workspaceChanged, WorkspaceEvent{Workspace: wsID})
	if firstPass {
		w.Commit("Pass " + taskID)
		d, _ := w.Task(taskID)
		shown := fmt.Sprintf("You passed “%s” (%d/%d tests).", d.Title, p, t)
		a.post(w, workspace.ChatItem{Thread: taskID, Role: workspace.RoleNotice, Text: shown})
		// Writers keep the next objectives ready, so the learner never waits;
		// the planning tutor records what was learned.
		a.keepReady(w, fmt.Sprintf("The learner just passed “%s”.", d.Title))
		a.send(w, workspace.PlanThread, message{
			Role:  workspace.RoleNotice,
			Shown: shown,
			Agent: fmt.Sprintf("[app] I passed task %s on submit (%d/%d tests). %s Record what I learned if there's evidence worth recording. Writers are preparing what comes next, so don't write lessons or tasks. Reply in one sentence at most.", taskID, p, t, hintSummary(w, taskID)),
		})
	}
	return res, nil
}

// hintSummary states exactly how many hints the learner used on a task, so
// the tutor doesn't have to guess from the chat.
func hintSummary(w *workspace.Workspace, taskID string) string {
	h := w.Hints(taskID)
	if h.Count == 0 {
		return "I used no hints."
	}
	return fmt.Sprintf("I used %d hint(s) on this task, highest level %d.", h.Count, h.Highest)
}

// taskKey names one task of one workspace.
type taskKey struct{ ws, task string }
