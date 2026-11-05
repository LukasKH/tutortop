package workspace_test

import (
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// newWorkspace makes a workspace in temporary folders.
func newWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	s := &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	w, err := s.Create("Learn Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// approve makes a Go plan with one module holding the given objectives the
// curriculum.
func approve(t *testing.T, w *workspace.Workspace, objectives ...string) *workspace.Curriculum {
	t.Helper()
	if err := w.Propose(plan(objectives...)); err != nil {
		t.Fatal(err)
	}
	c, err := w.ApproveProposal()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func plan(objectives ...string) workspace.Curriculum {
	m := workspace.Module{ID: "basics", Title: "Basics", References: []workspace.Source{{Title: "Go docs", URL: "https://go.dev/doc/"}}}
	for _, id := range objectives {
		m.Objectives = append(m.Objectives, workspace.Objective{ID: id, Name: "Objective " + id, Title: "Prove " + id})
	}
	return workspace.Curriculum{Language: "go", Modules: []workspace.Module{m}}
}

// addTask saves a task for an objective, as if it had passed the gate.
func addTask(t *testing.T, w *workspace.Workspace, objective string) workspace.TaskInfo {
	t.Helper()
	info, err := w.SaveTask(taskSpec(objective), "go", sandbox.TestNames{Visible: []string{"TestSum"}, Hidden: []string{"TestSumHidden"}})
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// taskSpec is a Go task that passes ValidateSpec.
func taskSpec(objective string) workspace.TaskSpec {
	return workspace.TaskSpec{
		ObjectiveID: objective,
		Title:       "Sum " + objective,
		Statement: workspace.Statement{
			Task:         "Write Sum so that it returns a + b.",
			Signature:    "func Sum(a, b int) int",
			Requirements: []string{"Sum returns a + b."},
			Examples:     []workspace.Example{{Input: "Sum(1, 2)", Output: "3"}, {Input: "Sum(-1, 1)", Output: "0"}},
			Context:      "Totals in your budget app.",
		},
		Starter:     sandbox.Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return 0 }\n"},
		Reference:   sandbox.Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a + b }\n"},
		Tests:       sandbox.Files{"sum_test.go": "package task\n"},
		HiddenTests: sandbox.Files{"sum_hidden_test.go": "package task\n"},
		Mutants: []sandbox.Mutant{
			{Name: "Off by one", Files: sandbox.Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a + b + 1 }\n"}},
			{Name: "Ignores b", Files: sandbox.Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a }\n"}},
		},
	}
}

// addLesson saves a three-question lesson for an objective.
func addLesson(t *testing.T, w *workspace.Workspace, objective string) workspace.LessonInfo {
	t.Helper()
	info, err := w.SaveLesson(lessonSpec(objective))
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// lessonSpec is a lesson that passes ValidateLesson. The answers are 0, 1
// and 2.
func lessonSpec(objective string) workspace.LessonSpec {
	return workspace.LessonSpec{
		ObjectiveID:   objective,
		Title:         "Adding numbers",
		Summary:       "You'll add two numbers in Go.",
		Body:          "## Adding\n\nUse `+`, as [the spec](https://go.dev/ref/spec#Arithmetic_operators) says:\n\n```go\n# not a heading inside code\nx := 1 + 2\n```\n", // the "#" line tests that headings inside code aren't read as headings
		PrimarySource: workspace.Source{Title: "The Go spec", URL: "https://go.dev/ref/spec"},
		Quiz: []workspace.QuizQuestion{
			{Question: "What is 1 + 2?", Options: []string{"three", "four", "five"}, Answer: 0, Explanation: "One plus two is three."},
			{Question: "What is 2 + 2?", Options: []string{"three", "four", "five"}, Answer: 1, Explanation: "Two plus two is four."},
			{Question: "What is 2 + 3?", Options: []string{"three", "four", "five"}, Answer: 2, Explanation: "Two plus three is five."},
		},
	}
}

func record(t *testing.T, w *workspace.Workspace, attempts ...workspace.Attempt) {
	t.Helper()
	for _, a := range attempts {
		if err := w.RecordAttempt(a); err != nil {
			t.Fatal(err)
		}
	}
}

// wantProblems fails unless err mentions every one of want, or, with no
// want, unless err is nil.
func wantProblems(t *testing.T, err error, want ...string) {
	t.Helper()
	if len(want) == 0 {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("no error, want one mentioning %q", want)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error doesn't mention %q:\n%v", w, err)
		}
	}
}
