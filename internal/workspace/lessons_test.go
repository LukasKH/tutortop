package workspace

import (
	"os"
	"strings"
	"testing"
)

func lessonWorkspace(t *testing.T) *Workspace {
	t.Helper()
	s := &Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	w, err := s.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Propose(Curriculum{Language: "go", Modules: []Module{{ID: "m", Title: "M", References: []Source{{Title: "Go docs", URL: "https://go.dev/doc/"}}, Objectives: []Objective{{ID: "go.errors", Name: "Errors", Title: "Errors"}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
	return w
}

func goodLesson() LessonSpec {
	return LessonSpec{
		ObjectiveID:   "go.errors",
		Title:         "Errors are values",
		Summary:       "You'll return and check errors the Go way.",
		Body:          "Go returns errors as values ([Effective Go](https://go.dev/doc/effective_go#errors)).\n\n```go\nf, err := os.Open(name)\nif err != nil {\n\treturn err\n}\n```\n",
		PrimarySource: Source{Title: "Error handling and Go", URL: "https://go.dev/blog/error-handling-and-go"},
		Quiz: []QuizQuestion{
			{Question: "What does a nil error mean?", Options: []string{"the call succeeded", "the call panicked", "the value is unset"}, Answer: 0, Explanation: "nil means no error happened."},
			{Question: "Where does the error go?", Options: []string{"first return value", "last return value", "a global variable"}, Answer: 1, Explanation: "By convention the error is last."},
			{Question: "How do you add context?", Options: []string{"panic with message", "log and continue", "wrap with %w"}, Answer: 2, Explanation: "fmt.Errorf with %w keeps the cause."},
		},
	}
}

func TestLessonQuizFlow(t *testing.T) {
	w := lessonWorkspace(t)
	spec := goodLesson()
	if err := w.ValidateLesson(spec); err != nil {
		t.Fatal(err)
	}
	info, err := w.SaveLesson(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.ValidateLesson(spec); err == nil || !strings.Contains(err.Error(), "already has") {
		t.Fatalf("a second lesson for the objective was accepted: %v", err)
	}

	// The public files never contain the answers.
	readme, _ := os.ReadFile(w.path("lessons", info.ID, "README.md"))
	if strings.Contains(string(readme), "Explanation") || strings.Contains(string(readme), "By convention") {
		t.Fatal("README leaks the explanations")
	}
	d, err := w.Lesson(info.ID)
	if err != nil || len(d.Quiz) != 3 || len(d.Results) != 0 {
		t.Fatalf("detail = %+v, %v", d, err)
	}

	r, err := w.AnswerQuiz(info.ID, 1, 0) // wrong
	if err != nil || r.Correct || r.Answer != 1 || r.Explanation == "" {
		t.Fatalf("wrong answer = %+v, %v", r, err)
	}
	// Only the first answer counts.
	if r, _ := w.AnswerQuiz(info.ID, 1, 1); r.Correct || r.Choice != 0 {
		t.Fatalf("second try changed the result: %+v", r)
	}
	if r, _ := w.AnswerQuiz(info.ID, 0, 0); !r.Correct {
		t.Fatalf("right answer = %+v", r)
	}
	st, _ := w.LessonStatuses()
	if len(st) != 1 || st[0].Answered != 2 || st[0].Correct != 1 || st[0].Done() {
		t.Fatalf("status = %+v", st)
	}
	// Quizzes never count as progress.
	if p := w.Progress(); p.Passed != 0 {
		t.Fatalf("progress = %+v", p)
	}
}

func TestQuizRulesGiveNothingAway(t *testing.T) {
	w := lessonWorkspace(t)
	spec := goodLesson()
	spec.Quiz[0].Options = []string{"the call succeeded without any problem at all", "it panicked", "unset"}
	for i := range spec.Quiz {
		spec.Quiz[i].Answer = 0
	}
	spec.Body = "no example, no links"
	err := w.ValidateLesson(spec)
	if err == nil {
		t.Fatal("want problems")
	}
	for _, want := range []string{"same number of words", "clearly the longest", "same position", "code block", "citations"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

// The app shows a lesson's title, summary and Read next itself, so a body
// that repeats them is sent back. A # inside a code block is a comment.
func TestLessonBodyDoesntRepeatTheFrame(t *testing.T) {
	w := lessonWorkspace(t)
	for name, body := range map[string]string{
		"title":     "# Errors are values\n\nGo returns errors ([doc](https://go.dev/doc/)).\n\n```go\nx := 1\n```\n",
		"read next": "Go returns errors ([doc](https://go.dev/doc/)).\n\n```go\nx := 1\n```\n\n## Read next\n\n[More](https://go.dev/blog/)\n",
	} {
		spec := goodLesson()
		spec.Body = body
		if err := w.ValidateLesson(spec); err == nil {
			t.Errorf("a body repeating the %s was accepted", name)
		}
	}
	spec := goodLesson()
	spec.Body = "Comments ([doc](https://go.dev/doc/)).\n\n```python\n# a comment, not a heading\nx = 1\n```\n"
	if err := w.ValidateLesson(spec); err != nil {
		t.Errorf("a # comment in code was taken for a heading: %v", err)
	}
}
