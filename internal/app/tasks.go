package app

import (
	"context"
	"fmt"

	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// TaskService serves the exercises: a task, the learner's code in it, and its
// test runs.
type TaskService struct {
	store   *workspace.Store
	runner  *sandbox.Runner
	agents  *threadAgents // tells the plan's tutor about a first pass
	writers *writers      // keep the next objectives written after a pass
}

func newTaskService(store *workspace.Store, runner *sandbox.Runner, agents *threadAgents, writers *writers) *TaskService {
	return &TaskService{store: store, runner: runner, agents: agents, writers: writers}
}

func (s *TaskService) Task(wsID, taskID string) (workspace.TaskDetail, error) {
	w, err := s.store.Open(wsID)
	if err != nil {
		return workspace.TaskDetail{}, err
	}
	return w.Task(taskID)
}

func (s *TaskService) SaveWork(wsID, taskID string, files sandbox.Files) error {
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	return w.SaveWork(taskID, files)
}

func (s *TaskService) ResetWork(wsID, taskID string) error {
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	return w.ResetWork(taskID)
}

// RunTests runs the visible tests, or all tests when submit is true.
// A passing submission tells the planning tutor, and writers write what
// comes next.
func (s *TaskService) RunTests(wsID, taskID string, files sandbox.Files, submit bool) (sandbox.Result, error) {
	w, err := s.store.Open(wsID)
	if err != nil {
		return sandbox.Result{}, err
	}
	if err := w.SaveWork(taskID, files); err != nil {
		return sandbox.Result{}, err
	}
	job, err := w.Job(taskID, submit)
	if err != nil {
		return sandbox.Result{}, err
	}
	res, err := s.runner.Run(context.Background(), job)
	if err != nil {
		return res, err
	}
	_ = w.SaveLastRun(taskID, res)
	kind := workspace.AttemptRun
	if submit {
		kind = workspace.AttemptSubmit
	}
	p, t := res.Counts()
	firstPass := submit && res.Status == sandbox.StatusPassed && !w.TaskPassed(taskID)
	_ = w.RecordAttempt(workspace.Attempt{TaskID: taskID, Kind: kind, Status: res.Status, Passed: p, Total: t})
	emit(workspaceChanged, WorkspaceEvent{Workspace: wsID})
	if firstPass {
		w.Commit("Pass " + taskID)
		d, _ := w.Task(taskID)
		shown := fmt.Sprintf("You passed “%s” (%d/%d tests).", d.Title, p, t)
		post(w, workspace.ChatItem{Thread: taskID, Role: workspace.RoleNotice, Text: shown})
		// Writers keep the next objectives ready, so the learner never waits;
		// the planning tutor records what was learned.
		s.writers.keepReady(w, fmt.Sprintf("The learner just passed “%s”.", d.Title))
		s.agents.send(w, workspace.PlanThread, message{
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
