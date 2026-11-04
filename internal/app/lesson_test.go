package app

import (
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
)

// A lesson's own chat knows which lesson it's about and how the quiz went.
func TestLessonThreadKnowsItsLesson(t *testing.T) {
	a := newTestApp(t)
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	planned(t, w, "go.errors")
	q := func(question string, options []string, answer int) workspace.QuizQuestion {
		return workspace.QuizQuestion{Question: question, Options: options, Answer: answer, Explanation: "Because."}
	}
	info, err := w.SaveLesson(workspace.LessonSpec{
		ObjectiveID: "go.errors", Title: "Errors are values", Summary: "Return and check errors.",
		Body:          "Errors are values ([Effective Go](https://go.dev/doc/effective_go#errors)).",
		PrimarySource: workspace.Source{Title: "Error handling and Go", URL: "https://go.dev/blog/error-handling-and-go"},
		Quiz: []workspace.QuizQuestion{
			q("A nil error means?", []string{"the call succeeded", "the call panicked", "the value unset"}, 0),
			q("Where is the error?", []string{"first return value", "last return value", "a global variable"}, 1),
			q("Add context how?", []string{"panic with message", "log then continue", "wrap with %w"}, 2),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.AnswerQuiz(info.ID, 2, 0); err != nil {
		t.Fatal(err)
	}

	thread := workspace.LessonThread(info.ID)
	if role := instructions(w, thread); !strings.Contains(role, "tutor for one lesson") || !strings.Contains(role, "Errors are values") {
		t.Fatalf("role = %s", role)
	}
	if c := contextFor(w, thread); !strings.Contains(c, "1 of 3 answered, 0 correct") || !strings.Contains(c, "question 3 wrong") {
		t.Fatalf("context = %s", c)
	}
}
