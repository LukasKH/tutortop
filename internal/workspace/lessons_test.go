package workspace_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
)

func TestValidateLesson(t *testing.T) {
	cases := []struct {
		name   string
		change func(s *workspace.LessonSpec)
		want   []string // none for a valid lesson
	}{
		{"valid", func(*workspace.LessonSpec) {}, nil},
		{"repeats the title", func(s *workspace.LessonSpec) { s.Body = "# Adding numbers\n\n" + s.Body }, []string{"body repeats the title"}},
		{"answer out of range", func(s *workspace.LessonSpec) { s.Quiz[1].Answer = 3 }, []string{"question 2: answer must be an option index (0-2)"}},
		{"no sources", func(s *workspace.LessonSpec) { s.Sources = nil }, []string{"sources needs the 1-8 pages"}},
		{"source that isn't a web link", func(s *workspace.LessonSpec) { s.Sources[0].URL = "go.dev" }, []string{"needs a full http(s) url"}},
	}
	w := newWorkspace(t)
	approve(t, w, "a", "b")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := lessonSpec("a")
			c.change(&spec)
			wantProblems(t, w.ValidateLesson(spec), c.want...)
		})
	}

	addLesson(t, w, "a")
	wantProblems(t, w.ValidateLesson(lessonSpec("a")), `objective "a" already has the lesson`)
	wantProblems(t, w.ValidateLesson(lessonSpec("b")))
}

// Only the first answer to a question counts, also when two reach the log.
// Answering again returns that first result, and answers stay hidden until a
// question is answered.
func TestAnswerQuiz(t *testing.T) {
	w := newWorkspace(t)
	approve(t, w, "a")
	l := addLesson(t, w, "a") // answers 0, 1, 2

	r, err := w.AnswerQuiz(l.ID, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r.Correct || r.Choice != 2 || r.Answer != 0 || r.Explanation == "" {
		t.Errorf("wrong first answer: %+v", r)
	}
	again, err := w.AnswerQuiz(l.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if again != r {
		t.Errorf("answering again gave %+v, want the first result %+v", again, r)
	}
	if r, _ := w.AnswerQuiz(l.ID, 1, 1); !r.Correct {
		t.Errorf("right answer marked wrong: %+v", r)
	}

	// Two answers sent at once can both reach the log.
	record(t, w, workspace.Attempt{LessonID: l.ID, Kind: workspace.AttemptQuiz, Question: 1, Choice: 0})

	d, err := w.Lesson(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Answered != 2 || d.Correct != 1 || d.Done() || len(d.Quiz) != 3 {
		t.Errorf("detail: answered %d, correct %d, %d questions", d.Answered, d.Correct, len(d.Quiz))
	}
	if _, shown := d.Results[2]; shown || d.Results[0].Answer != 0 {
		t.Errorf("results %+v, want only questions 0 and 1 with their answers", d.Results)
	}

	if _, err := w.AnswerQuiz(l.ID, 2, 2); err != nil {
		t.Fatal(err)
	}
	statuses, err := w.LessonStatuses()
	if err != nil || len(statuses) != 1 {
		t.Fatalf("statuses %+v, %v", statuses, err)
	}
	if s := statuses[0]; s.Answered != 3 || s.Correct != 2 || !s.Done() {
		t.Errorf("status: answered %d, correct %d, done %v", s.Answered, s.Correct, s.Done())
	}
}

// A lesson keeps the sources it's based on, for its Sources tab and its
// README. Lessons saved before sources were kept still open, with none.
func TestLessonSources(t *testing.T) {
	w := newWorkspace(t)
	approve(t, w, "a", "b")
	spec := lessonSpec("a")
	l := addLesson(t, w, "a")

	d, err := w.Lesson(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.Sources, spec.Sources) {
		t.Errorf("sources = %+v, want %+v", d.Sources, spec.Sources)
	}
	readme, err := os.ReadFile(filepath.Join(w.Dir, "lessons", l.ID, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "## Sources\n\n- [" + spec.Sources[0].Title + "](" + spec.Sources[0].URL + ")"; !strings.Contains(string(readme), want) {
		t.Errorf("README.md lacks %q:\n%s", want, readme)
	}

	old := addLesson(t, w, "b")
	before := "summary: Old.\nbody: Old.\nprimary_source:\n  title: Go\n  url: https://go.dev/\nquiz: []\n"
	if err := os.WriteFile(filepath.Join(w.PrivateDir, "lessons", old.ID, "lesson.yaml"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err = w.Lesson(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(d.Sources); string(b) != "[]" || d.PrimarySource.URL != "https://go.dev/" {
		t.Errorf("old lesson: sources %s, primary source %+v", b, d.PrimarySource)
	}
}
