package workspace_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
)

func TestValidateSpec(t *testing.T) {
	cases := []struct {
		name   string
		change func(s *workspace.TaskSpec)
		want   []string // none for a valid spec
	}{
		{"valid", func(*workspace.TaskSpec) {}, nil},
		{"test in the starter", func(s *workspace.TaskSpec) {
			s.Starter["sum_test.go"], s.Reference["sum_test.go"] = "", ""
		}, []string{"starter file sum_test.go looks like a test file", "sum_test.go is both a test and a starter file"}},
		// b's own python, not the plan's go, judges the file names.
		{"objective in its own language", func(s *workspace.TaskSpec) { s.ObjectiveID = "b" }, []string{"sum_test.go isn't named like a python test file"}},
		{"every problem at once", func(s *workspace.TaskSpec) {
			s.Title = ""
			s.Statement.Context = ""
		}, []string{"title is required", "statement.context is required"}},
	}
	w := newWorkspace(t)
	c := plan("a", "b")
	c.Modules[0].Objectives[1].Language = "python"
	if err := w.Propose(c); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := taskSpec("a")
			c.change(&spec)
			spec, tc, err := w.ValidateSpec(spec)
			wantProblems(t, err, c.want...)
			if err == nil && (tc.Language != "go" || spec.Size != "medium") {
				t.Errorf("toolchain %q, size %q; want go and the plan's default size", tc.Language, spec.Size)
			}
		})
	}
}

// Saved tasks and lessons keep their answers private: the reference, hidden
// tests, mutants and quiz answers live outside the workspace, and grading
// reads a private copy of the visible tests.
func TestSaveTask(t *testing.T) {
	w := newWorkspace(t)
	approve(t, w, "a")
	first := addTask(t, w, "a")
	spec := taskSpec("a")
	spec.Title = "Sum, again!"
	second, err := w.SaveTask(spec, "go", sandbox.TestNames{Visible: []string{"TestSum"}, Hidden: []string{"TestA", "TestB"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "0001-sum-a" || second.ID != "0002-sum-again" {
		t.Errorf("ids %q, %q; want numbered slugs", first.ID, second.ID)
	}
	addLesson(t, w, "a")

	err = filepath.WalkDir(w.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.Contains(p, "hidden") || strings.Contains(p, "reference") || strings.Contains(p, "mutants") {
			t.Errorf("private file in the workspace: %s", p)
		}
		if b, _ := os.ReadFile(p); strings.Contains(string(b), "One plus two is three") || strings.Contains(string(b), "answer:") {
			t.Errorf("%s gives away a quiz answer", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Editing the public tests can't change what a check runs.
	if err := os.WriteFile(filepath.Join(w.Dir, "tasks", second.ID, "tests", "sum_test.go"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	run, err := w.Job(second.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if run.Tests["sum_test.go"] != spec.Tests["sum_test.go"] || len(run.HiddenTests) != 0 || len(run.Names.Hidden) != 2 {
		t.Errorf("run job: tests %v, hidden %v, names %+v", run.Tests, run.HiddenTests, run.Names)
	}
	sub, err := w.Job(second.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if sub.HiddenTests["sum_hidden_test.go"] != spec.HiddenTests["sum_hidden_test.go"] {
		t.Errorf("submit job hidden tests %v", sub.HiddenTests)
	}
}

// The learner may only change the task's editable files.
func TestSaveWork(t *testing.T) {
	w := newWorkspace(t)
	approve(t, w, "a")
	task := addTask(t, w, "a")
	solved := "package task\n\nfunc Sum(a, b int) int { return a + b }\n"

	for _, files := range []sandbox.Files{
		{"sum_test.go": "package task\n"},
		{"../sum.go": solved},
	} {
		if err := w.SaveWork(task.ID, files); err == nil {
			t.Errorf("saved %v", slices.Sorted(maps.Keys(files)))
		}
	}
	if err := w.SaveWork(task.ID, sandbox.Files{"sum.go": solved}); err != nil {
		t.Fatal(err)
	}
	if work, _ := w.Work(task.ID); work["sum.go"] != solved || len(work) != 1 {
		t.Errorf("work %v", work)
	}

	if err := w.ResetWork(task.ID); err != nil {
		t.Fatal(err)
	}
	if work, _ := w.Work(task.ID); work["sum.go"] != taskSpec("a").Starter["sum.go"] {
		t.Errorf("reset work %v, want the starter", work)
	}
}
