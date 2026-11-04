package mcp_test

import (
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
)

func TestCreateLessonTool(t *testing.T) {
	tt := newTools(t)
	w := tt.create(t, "Go")
	planned(t, w, "go.errors")
	q := func(question string, options []string, answer int) map[string]any {
		return map[string]any{"question": question, "options": options, "answer": answer, "explanation": "Because."}
	}
	out, isErr := tt.caller(t, w, workspace.WriterThread("go.errors"))("create_lesson", map[string]any{
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
	if lessons, err := w.LessonStatuses(); err != nil || len(lessons) != 1 || lessons[0].Questions != 3 {
		t.Fatalf("lessons = %+v, %v", lessons, err)
	}
}
