package app

import (
	"context"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCreateLessonTool(t *testing.T) {
	a := newTestApp(t)
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Propose(workspace.Curriculum{Language: "go", Modules: []workspace.Module{{ID: "m", Title: "M", References: []workspace.Source{{Title: "Go docs", URL: "https://go.dev/doc/"}}, Objectives: []workspace.Objective{{ID: "go.errors", Name: "Errors", Title: "Errors"}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{Endpoint: a.mcp.URL(w, workspace.WriterThread("go.errors"))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	call := func(args map[string]any) (string, bool) {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "create_lesson", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return res.Content[0].(*mcp.TextContent).Text, res.IsError
	}
	q := func(question string, options []string, answer int) map[string]any {
		return map[string]any{"question": question, "options": options, "answer": answer, "explanation": "Because."}
	}
	lesson := map[string]any{
		"objectiveId": "go.errors", "title": "Errors are values", "summary": "Return and check errors.",
		"body":          "Errors are values ([Effective Go](https://go.dev/doc/effective_go#errors)).\n\n```go\nif err != nil {\n\treturn err\n}\n```",
		"primarySource": map[string]any{"title": "Error handling and Go", "url": "https://go.dev/blog/error-handling-and-go"},
		"quiz": []any{
			q("A nil error means?", []string{"the call succeeded", "the call panicked", "the value unset"}, 0),
			q("Where is the error?", []string{"first return value", "last return value", "a global variable"}, 1),
			q("Add context how?", []string{"panic with message", "log then continue", "wrap with %w"}, 2),
		},
	}

	bad := map[string]any{}
	for k, v := range lesson {
		bad[k] = v
	}
	bad["quiz"] = []any{q("Only one?", []string{"yes it is", "no", "maybe"}, 0)}
	if out, isErr := call(bad); !isErr || !strings.Contains(out, "3-5 questions") {
		t.Fatalf("want a rejection, got %s", out)
	}
	if out, isErr := call(lesson); isErr {
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
	if role := roleInstructions(w, thread); !strings.Contains(role, "tutor for one lesson") || !strings.Contains(role, "Errors are values") {
		t.Fatalf("role = %s", role)
	}
	if c := a.taskContext(w, thread); !strings.Contains(c, "1 of 3 answered, 0 correct") || !strings.Contains(c, "question 3 wrong") {
		t.Fatalf("context = %s", c)
	}
}
