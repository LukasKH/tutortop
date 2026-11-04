package workspace_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Only a submission that passed every test is progress. Runs, failed
// submits and hints never are, and a pass stays a pass.
func TestProgressCountsOnlyPassedSubmissions(t *testing.T) {
	w := newWorkspace(t)
	approve(t, w, "a", "b", "c", "d")
	ta := addTask(t, w, "a")
	tb := addTask(t, w, "b")
	tc := addTask(t, w, "c")

	record(t, w,
		workspace.Attempt{TaskID: ta.ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed},
		workspace.Attempt{TaskID: ta.ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusFailed}, // a later failure doesn't undo it
		workspace.Attempt{TaskID: tb.ID, Kind: workspace.AttemptRun, Status: sandbox.StatusPassed},
		workspace.Attempt{TaskID: tb.ID, Kind: workspace.AttemptHint, HintLevel: 2},
		workspace.Attempt{TaskID: tc.ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusError},
	)

	p := w.Progress()
	if p.Passed != 1 || p.Total != 4 || p.PassedEver != 1 {
		t.Fatalf("passed %d of %d, ever %d; want 1 of 4, ever 1", p.Passed, p.Total, p.PassedEver)
	}
	want := map[string]workspace.ObjectiveStatus{"a": workspace.ObjectivePassed, "b": workspace.ObjectiveInProgress, "c": workspace.ObjectiveInProgress, "d": workspace.ObjectivePlanned}
	for id, status := range want {
		if got := p.Objectives[id].Status; got != status {
			t.Errorf("objective %s is %q, want %q", id, got, status)
		}
	}
	if !slices.Equal(p.Objectives["a"].Tasks, []string{ta.ID}) {
		t.Errorf("objective a lists tasks %v, want [%s]", p.Objectives["a"].Tasks, ta.ID)
	}
	// The frontend reads tasks as a list, so an objective without any is [], not null.
	b, _ := json.Marshal(p.Objectives["d"])
	if !strings.Contains(string(b), `"tasks":[]`) {
		t.Errorf("objective without tasks encodes as %s", b)
	}
}

// Changing the plan never looks like losing progress: an objective passed
// and then dropped still counts in PassedEver.
func TestProgressSurvivesPlanChanges(t *testing.T) {
	w := newWorkspace(t)
	approve(t, w, "a", "b")
	ta := addTask(t, w, "a")
	tb := addTask(t, w, "b")
	record(t, w,
		workspace.Attempt{TaskID: ta.ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed},
		workspace.Attempt{TaskID: tb.ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed},
	)

	approve(t, w, "b", "c") // a is dropped, b kept

	p := w.Progress()
	if p.Passed != 1 || p.Total != 2 || p.PassedEver != 2 {
		t.Fatalf("passed %d of %d, ever %d; want 1 of 2, ever 2", p.Passed, p.Total, p.PassedEver)
	}
	if _, ok := p.Objectives["a"]; ok {
		t.Error("a dropped objective still shows in the plan's progress")
	}
	if p.Objectives["b"].Status != workspace.ObjectivePassed {
		t.Errorf("kept objective b is %q, want passed", p.Objectives["b"].Status)
	}
}

func TestProgressWithoutPlan(t *testing.T) {
	w := newWorkspace(t)
	p := w.Progress()
	if p.Total != 0 || p.Objectives == nil {
		t.Fatalf("progress without a plan: %+v", p)
	}
}

func TestTaskStatusesAndHints(t *testing.T) {
	w := newWorkspace(t)
	approve(t, w, "a", "b")
	ta := addTask(t, w, "a")
	tb := addTask(t, w, "b")
	record(t, w,
		workspace.Attempt{TaskID: ta.ID, Kind: workspace.AttemptHint, HintLevel: 2},
		workspace.Attempt{TaskID: ta.ID, Kind: workspace.AttemptHint, HintLevel: 1},
		workspace.Attempt{TaskID: ta.ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed},
	)

	statuses, err := w.TaskStatuses()
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].ID != ta.ID || statuses[1].ID != tb.ID {
		t.Fatalf("statuses not in task order: %+v", statuses)
	}
	if !statuses[0].Passed || statuses[0].HintLevel != 2 || statuses[1].Passed || statuses[1].HintLevel != 0 {
		t.Errorf("statuses: %+v", statuses)
	}
	if h := w.Hints(ta.ID); h != (workspace.HintUse{Count: 2, Highest: 2}) {
		t.Errorf("hints %+v, want 2 asked, deepest 2", h)
	}
}

// Writers keep the next unpassed objectives ready: each needs a lesson and
// a task the learner hasn't solved. Passed objectives don't use up the
// look-ahead.
func TestNeedsWriting(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, w *workspace.Workspace)
		ahead int
		want  []string
	}{
		{"nothing written", nil, 2, []string{"a", "b"}},
		{"look-ahead of zero", nil, 0, nil},
		{"lesson and task ready", func(t *testing.T, w *workspace.Workspace) {
			addLesson(t, w, "a")
			addTask(t, w, "a")
		}, 2, []string{"b"}},
		{"lesson without a task", func(t *testing.T, w *workspace.Workspace) {
			addLesson(t, w, "a")
		}, 1, []string{"a"}},
		{"task without a lesson", func(t *testing.T, w *workspace.Workspace) {
			addTask(t, w, "a")
		}, 1, []string{"a"}},
		{"passed objective is skipped", func(t *testing.T, w *workspace.Workspace) {
			ta := addTask(t, w, "a")
			record(t, w, workspace.Attempt{TaskID: ta.ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed})
		}, 2, []string{"b", "c"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorkspace(t)
			approve(t, w, "a", "b", "c")
			if c.setup != nil {
				c.setup(t, w)
			}
			var got []string
			for _, o := range w.NeedsWriting(c.ahead) {
				got = append(got, o.ID)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("needs writing %v, want %v", got, c.want)
			}
		})
	}

	if got := newWorkspace(t).NeedsWriting(2); got != nil {
		t.Errorf("without a plan, needs writing %v", got)
	}
}

// A task counts once, on its first pass; a lesson once every question has
// its first answer.
func TestCompletions(t *testing.T) {
	w := newWorkspace(t)
	approve(t, w, "a")
	ta := addTask(t, w, "a")
	lesson := addLesson(t, w, "a") // three questions
	day := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return day.Add(time.Duration(h) * time.Hour) }
	record(t, w,
		workspace.Attempt{TaskID: ta.ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed, At: at(1)},
		workspace.Attempt{TaskID: ta.ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed, At: at(2)},
		workspace.Attempt{LessonID: lesson.ID, Kind: workspace.AttemptQuiz, Question: 0, At: at(3)},
		workspace.Attempt{LessonID: lesson.ID, Kind: workspace.AttemptQuiz, Question: 0, At: at(4)},
		workspace.Attempt{LessonID: lesson.ID, Kind: workspace.AttemptQuiz, Question: 1, At: at(5)},
		workspace.Attempt{LessonID: lesson.ID, Kind: workspace.AttemptQuiz, Question: 2, At: at(6)},
	)

	cs, err := w.Completions()
	if err != nil {
		t.Fatal(err)
	}
	want := []workspace.Completion{{Kind: "task", At: at(1)}, {Kind: "lesson", At: at(6)}}
	if !slices.EqualFunc(cs, want, func(a, b workspace.Completion) bool { return a.Kind == b.Kind && a.At.Equal(b.At) }) {
		t.Errorf("completions %+v, want %+v", cs, want)
	}
}

func TestSummarizeStreaks(t *testing.T) {
	now := time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC)
	on := func(month time.Month, day int, kind string) workspace.Completion {
		return workspace.Completion{Kind: kind, At: time.Date(2026, month, day, 10, 0, 0, 0, time.UTC)}
	}
	cases := []struct {
		name                string
		cs                  []workspace.Completion
		today, streak, best int
	}{
		{"today only", []workspace.Completion{on(3, 10, "task"), on(3, 10, "lesson")}, 2, 1, 1},
		// Nothing yet today keeps yesterday's streak alive until midnight.
		{"up to yesterday", []workspace.Completion{on(3, 8, "task"), on(3, 9, "task")}, 0, 2, 2},
		{"broken two days ago", []workspace.Completion{on(3, 7, "task"), on(3, 8, "task")}, 0, 0, 2},
		{"best is an older run", []workspace.Completion{on(2, 27, "task"), on(2, 28, "task"), on(3, 1, "task"), on(3, 10, "task")}, 1, 1, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := workspace.Summarize(c.cs, now)
			if a.Today != c.today || a.Streak != c.streak || a.Best != c.best || a.Total != len(c.cs) {
				t.Errorf("today %d, streak %d, best %d, total %d; want %d, %d, %d, %d", a.Today, a.Streak, a.Best, a.Total, c.today, c.streak, c.best, len(c.cs))
			}
		})
	}
}
