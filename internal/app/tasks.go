package app

import (
	"context"
	"fmt"

	"github.com/LukasKH/tutortop/internal/agent/threads"
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// TaskService serves the exercises: a task, the learner's code in it, and its
// test runs.
type TaskService struct {
	store   *workspace.Store
	runner  *sandbox.Runner
	agents  *threads.Agents  // tells the plan's tutor about a first pass and the learner's feedback
	writers *threads.Writers // keep the next objectives written after a pass
}

func newTaskService(store *workspace.Store, runner *sandbox.Runner, agents *threads.Agents, writers *threads.Writers) *TaskService {
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
	events.Emit(events.WorkspaceChanged, events.Workspace{Workspace: wsID})
	if firstPass {
		s.announcePass(w, taskID, p, t)
	}
	return res, nil
}

// SendFeedback tells the planning tutor what the learner thought of an
// exercise, so later lessons and exercises fit them better.
func (s *TaskService) SendFeedback(wsID, taskID string, fb Feedback) error {
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	d, err := w.Task(taskID)
	if err != nil {
		return err
	}
	return sendFeedback(s.agents, s.writers, w, fmt.Sprintf("the exercise “%s”", d.Title), "", fb)
}

// announcePass marks a task's first pass: a commit, a notice in its chat,
// writers keeping the next objectives ready so the learner never waits, and
// the planning tutor recording what was learned.
func (s *TaskService) announcePass(w *workspace.Workspace, taskID string, passed, total int) {
	w.Commit("Pass " + taskID)
	d, _ := w.Task(taskID)
	shown := fmt.Sprintf("You passed “%s” (%d/%d tests).", d.Title, passed, total)
	threads.Post(w, workspace.ChatItem{Thread: taskID, Role: workspace.RoleNotice, Text: shown})
	s.writers.KeepReady(w, fmt.Sprintf("The learner just passed “%s”.", d.Title))
	s.agents.Send(w, workspace.PlanThread, threads.Message{
		Role:  workspace.RoleNotice,
		Shown: shown,
		Agent: fmt.Sprintf("[app] I passed task %s on submit (%d/%d tests). %s Record what I learned if there's evidence worth recording. Writers are preparing what comes next, so don't write lessons or tasks. Reply in one sentence at most.", taskID, passed, total, hintSummary(w, taskID)),
	})
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
