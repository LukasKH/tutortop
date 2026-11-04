package workspace_test

import (
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
		{"unknown objective", func(s *workspace.LessonSpec) { s.ObjectiveID = "missing" }, []string{`objective "missing" isn't in the curriculum`}},
		{"no summary", func(s *workspace.LessonSpec) { s.Summary = "" }, []string{"title and summary are required"}},
		{"no code block", func(s *workspace.LessonSpec) { s.Body = "## Adding\n\nSee [the spec](https://go.dev/ref/spec).\n" }, []string{"worked example in a code block"}},
		{"no citation", func(s *workspace.LessonSpec) { s.Body = "## Adding\n\n```go\nx := 1\n```\n" }, []string{"body needs citations"}},
		{"repeats the title", func(s *workspace.LessonSpec) { s.Body = "# Adding numbers\n\n" + s.Body }, []string{"body repeats the title"}},
		{"its own read next", func(s *workspace.LessonSpec) { s.Body += "\n## Read next\n" }, []string{"body has its own Read next"}},
		{"too long", func(s *workspace.LessonSpec) { s.Body += strings.Repeat("x", 9000) }, []string{"keep a lesson short"}},
		{"primary source isn't a link", func(s *workspace.LessonSpec) { s.PrimarySource.URL = "go.dev" }, []string{"primarySource needs a title and an http(s) url"}},
		{"two questions", func(s *workspace.LessonSpec) { s.Quiz = s.Quiz[:2] }, []string{"quiz needs 3-5 questions"}},
		{"two options", func(s *workspace.LessonSpec) { s.Quiz[0].Options = []string{"three", "four"} }, []string{"question 1 needs 3-4 options"}},
		{"answer out of range", func(s *workspace.LessonSpec) { s.Quiz[1].Answer = 3 }, []string{"question 2: answer must be an option index (0-2)"}},
		{"options of different lengths", func(s *workspace.LessonSpec) {
			s.Quiz[0].Options = []string{"exactly three it is", "four", "five"}
		}, []string{"question 1: options have 1 to 4 words"}},
		{"longest option is the answer", func(s *workspace.LessonSpec) {
			s.Quiz[0].Options = []string{"threeeeeeeee", "four", "five"}
		}, []string{"question 1: the correct option is clearly the longest"}},
		{"duplicate options", func(s *workspace.LessonSpec) { s.Quiz[2].Options = []string{"four", "Four ", "five"} }, []string{"question 3 has duplicate options"}},
		{"answer always first", func(s *workspace.LessonSpec) {
			for i := range s.Quiz {
				s.Quiz[i].Answer = 0
			}
		}, []string{"same position in every question"}},
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

// Only the first answer to a question counts. Answering again returns that
// first result, and answers stay hidden until a question is answered.
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

	for _, c := range []struct{ question, choice int }{{3, 0}, {-1, 0}, {2, 3}, {2, -1}} {
		if _, err := w.AnswerQuiz(l.ID, c.question, c.choice); err == nil {
			t.Errorf("question %d, choice %d was accepted", c.question, c.choice)
		}
	}
	if _, err := w.AnswerQuiz("0009-missing", 0, 0); err == nil {
		t.Error("answered a lesson that doesn't exist")
	}

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

// Two answers sent at once can both reach the log; the first still wins.
func TestQuizFirstLoggedAnswerWins(t *testing.T) {
	w := newWorkspace(t)
	approve(t, w, "a")
	l := addLesson(t, w, "a")
	record(t, w,
		workspace.Attempt{LessonID: l.ID, Kind: workspace.AttemptQuiz, Question: 0, Choice: 1},
		workspace.Attempt{LessonID: l.ID, Kind: workspace.AttemptQuiz, Question: 0, Choice: 0, Passed: 1},
	)
	d, err := w.Lesson(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r := d.Results[0]; r.Choice != 1 || r.Correct || d.Answered != 1 {
		t.Errorf("result %+v, answered %d; want the first, wrong answer once", r, d.Answered)
	}
}
