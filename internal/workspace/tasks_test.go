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
		{"unknown objective", func(s *workspace.TaskSpec) { s.ObjectiveID = "missing" }, []string{`objective "missing" isn't in the curriculum`}},
		{"starter named like a test", func(s *workspace.TaskSpec) {
			s.Starter["x_test.go"], s.Reference["x_test.go"] = "", ""
		}, []string{"starter file x_test.go looks like a test file"}},
		{"hidden test not named like a test", func(s *workspace.TaskSpec) { s.HiddenTests["edge.go"] = "" }, []string{"edge.go isn't named like a go test file"}},
		{"test is also a starter file", func(s *workspace.TaskSpec) { s.HiddenTests["sum.go"] = "" }, []string{"sum.go is both a test and a starter file"}},
		{"test both visible and hidden", func(s *workspace.TaskSpec) { s.HiddenTests["sum_test.go"] = "" }, []string{"sum_test.go is both a visible and a hidden test file"}},
		{"mutant changes a test", func(s *workspace.TaskSpec) {
			s.Mutants[0].Files = sandbox.Files{"sum_test.go": ""}
		}, []string{`mutant "Off by one" changes sum_test.go, which isn't a solution file`}},
		{"every problem at once", func(s *workspace.TaskSpec) {
			s.Title = ""
			s.Statement.Context = ""
		}, []string{"title is required", "statement.context is required"}},
	}
	w := newWorkspace(t)
	approve(t, w, "a")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := taskSpec("a")
			c.change(&spec)
			tc, err := w.ValidateSpec(&spec)
			wantProblems(t, err, c.want...)
			if err == nil && (tc.Language != "go" || spec.Size != "medium") {
				t.Errorf("toolchain %q, size %q; want go and the plan's default size", tc.Language, spec.Size)
			}
		})
	}
}

// A saved task keeps its answers private: the reference, hidden tests and
// mutants live outside the workspace, and grading reads a private copy of
// the visible tests.
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

	d, err := w.Task(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Work["sum.go"] != spec.Starter["sum.go"] || d.Tests["sum_test.go"] != spec.Tests["sum_test.go"] {
		t.Errorf("work %v and tests %v, want the starter and visible tests", d.Work, d.Tests)
	}
	if d.HiddenCount != 2 {
		t.Errorf("detail: hidden %d, want 2", d.HiddenCount)
	}

	err = filepath.WalkDir(w.Dir, func(p string, _ os.DirEntry, err error) error {
		if strings.Contains(p, "hidden") || strings.Contains(p, "reference") || strings.Contains(p, "mutants") {
			t.Errorf("private file in the workspace: %s", p)
		}
		return err
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
	if run.Tests["sum_test.go"] != spec.Tests["sum_test.go"] || len(run.HiddenTests) != 0 || run.Toolchain.Language != "go" {
		t.Errorf("run job: tests %v, hidden %v", run.Tests, run.HiddenTests)
	}
	if len(run.Names.Hidden) != 2 {
		t.Errorf("run job names %+v, want the saved names", run.Names)
	}
	sub, err := w.Job(second.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if sub.HiddenTests["sum_hidden_test.go"] != spec.HiddenTests["sum_hidden_test.go"] {
		t.Errorf("submit job hidden tests %v", sub.HiddenTests)
	}
}

func TestSaveTaskKeepsFilesInside(t *testing.T) {
	w := newWorkspace(t)
	approve(t, w, "a")
	spec := taskSpec("a")
	spec.Starter = sandbox.Files{"../../escape.go": "package task\n"}
	if _, err := w.SaveTask(spec, "go", sandbox.TestNames{}); err == nil {
		t.Fatal("saved a file outside the task folder")
	}
	if _, err := os.Stat(filepath.Join(w.Dir, "escape.go")); !os.IsNotExist(err) {
		t.Errorf("escape.go was written: %v", err)
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
		{"sum.go": solved, "new.go": "package task\n"},
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
