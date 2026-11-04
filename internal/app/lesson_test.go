package app

import (
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
)

func TestCreateLessonTool(t *testing.T) {
	a := newTestApp(t)
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	planned(t, w, "go.errors")
	q := func(question string, options []string, answer int) map[string]any {
		return map[string]any{"question": question, "options": options, "answer": answer, "explanation": "Because."}
	}
	out, isErr := toolCaller(t, a, w, workspace.WriterThread("go.errors"))("create_lesson", map[string]any{
		"objectiveId": "go.errors", "title": "Errors are values", "summary": "Return and check errors.",
		"body":          "Errors are values ([Effective Go](https://go.dev/doc/effective_go#errors)).\n\n```go\nif err != nil {\n\treturn err\n}\n```",
		"primarySource": map[string]any{"title": "Error handling and Go", "url": "https://go.dev/blog/error-handling-and-go"},
		"quiz": []any{
			q("A nil error means?", []string{"the call succeeded", "the call panicked", "the value unset"}, 0),
			q("Where is the error?", []string{"first return value", "last return value", "a global variable"}, 1),
			q("Add context how?", []string{"panic with message", "log then continue", "wrap with %w"}, 2),
		},
	})
	if isErr {
		t.Fatalf("create_lesson: %s", out)
	}

	st, err := a.Workspace(w.ID)
	if err != nil || len(st.Lessons) != 1 || st.Lessons[0].Questions != 3 {
		t.Fatalf("lessons = %+v, %v", st.Lessons, err)
	}
	id := st.Lessons[0].ID
	if _, err := a.AnswerQuiz(w.ID, id, 2, 0); err != nil {
		t.Fatal(err)
	}

	// A lesson's own chat knows which lesson it's about and how the quiz went.
	thread := workspace.LessonThread(id)
	if role := instructions(w, thread); !strings.Contains(role, "tutor for one lesson") || !strings.Contains(role, "Errors are values") {
		t.Fatalf("role = %s", role)
	}
	if c := a.contextFor(w, thread); !strings.Contains(c, "1 of 3 answered, 0 correct") || !strings.Contains(c, "question 3 wrong") {
		t.Fatalf("context = %s", c)
	}
}
