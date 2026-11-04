package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A thread's tools decide what its agent can change: chats talk with the
// learner and shape the plan, and only writers publish lessons and tasks.
func TestThreadsGetTheirKindsTools(t *testing.T) {
	a := newTestApp(t)
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	chat := []string{"ask_user", "get_state", "propose_curriculum", "set_icon", "set_toolchain"}
	writer := []string{"create_lesson", "create_task", "get_state"}
	for thread, want := range map[string][]string{
		workspace.PlanThread:                chat,
		"0001-some-task":                    chat,
		workspace.LessonThread("0001-x"):    chat,
		workspace.WriterThread("go.errors"): writer,
	} {
		var got []string
		for tool, err := range toolSession(t, a, w, thread).Tools(context.Background(), nil) {
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, tool.Name)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s has %v, want %v", thread, got, want)
		}
	}
}

// Each objective has its own writer, so one can't publish for another.
func TestWriterKeepsToItsObjective(t *testing.T) {
	a := newTestApp(t)
	w := goWorkspace(t, a)
	write := toolCaller(t, a, w, workspace.WriterThread("go.maps"))
	if out, isErr := write("create_task", reverseWordsTask("go.strings")); !isErr || !strings.Contains(out, `not "go.strings"`) {
		t.Fatalf("create_task for another objective: %s", out)
	}
	lesson := map[string]any{
		"objectiveId": "go.strings", "title": "Strings", "summary": "S.", "body": "B.",
		"primarySource": map[string]any{"title": "Go", "url": "https://go.dev/"}, "quiz": []any{},
	}
	if out, isErr := write("create_lesson", lesson); !isErr || !strings.Contains(out, `not "go.strings"`) {
		t.Fatalf("create_lesson for another objective: %s", out)
	}
	if lessons, _ := w.Lessons(); len(lessons) != 0 {
		t.Fatalf("lessons = %+v", lessons)
	}
}

func TestAskUserTakesOneToFourQuestions(t *testing.T) {
	a := newTestApp(t)
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	ask := toolCaller(t, a, w, "0001-some-task")
	questions := func(n int) map[string]any {
		qs := []any{}
		for range n {
			qs = append(qs, map[string]any{"id": "q", "question": "Which?"})
		}
		return map[string]any{"questions": qs}
	}
	for _, n := range []int{0, 5} {
		if out, isErr := ask("ask_user", questions(n)); !isErr {
			t.Fatalf("%d questions were shown: %s", n, out)
		}
	}
	if w.PendingQuestions() != nil {
		t.Fatal("a refused form was saved")
	}
	if out, isErr := ask("ask_user", questions(4)); isErr {
		t.Fatalf("4 questions: %s", out)
	}
	// The answers go back to the thread that asked.
	if q := w.PendingQuestions(); q == nil || q.Thread != "0001-some-task" || len(q.Questions) != 4 {
		t.Fatalf("pending = %+v", q)
	}
}

// A task whose tests don't catch its mutants stays unpublished, and the
// writer gets the gate's report to fix it.
func TestCreateTaskWithholdsATaskThatFailsTheGate(t *testing.T) {
	a := newTestApp(t)
	if err := a.runner.Check(context.Background()); err != nil {
		t.Skip(err)
	}
	w := goWorkspace(t, a)
	spec := reverseWordsTask("go.strings")
	spec["mutants"] = []any{
		map[string]any{"name": "same", "description": "the reference itself", "files": map[string]string{"solution.go": goReference}},
		map[string]any{"name": "same-again", "description": "the reference again", "files": map[string]string{"solution.go": goReference}},
	}
	out, isErr := toolCaller(t, a, w, workspace.WriterThread("go.strings"))("create_task", spec)
	var res createTaskOutput
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !isErr || res.Published || res.Gate == nil || res.Gate.Passed || !strings.Contains(res.Report, "same") {
		t.Fatalf("isErr %v, result %+v", isErr, res)
	}
	if tasks, _ := w.Tasks(); len(tasks) != 0 {
		t.Fatalf("tasks = %+v", tasks)
	}
}

// goWorkspace is a workspace with an approved Go curriculum of two objectives.
func goWorkspace(t *testing.T, a *App) *workspace.Workspace {
	t.Helper()
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Propose(workspace.Curriculum{Language: "go", Modules: []workspace.Module{{
		ID: "basics", Title: "Basics", References: []workspace.Source{{Title: "Docs", URL: "https://go.dev/doc/"}},
		Objectives: []workspace.Objective{{ID: "go.strings", Name: "Strings", Title: "Strings"}, {ID: "go.maps", Name: "Maps", Title: "Maps"}},
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
	return w
}

// toolCaller connects to one thread's MCP tools, the way that thread's agent
// would, and returns a function that calls a tool and reports its text and
// whether it was an error.
func toolCaller(t *testing.T, a *App, w *workspace.Workspace, thread string) func(name string, args any) (string, bool) {
	t.Helper()
	cs := toolSession(t, a, w, thread)
	return func(name string, args any) (string, bool) {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return b.String(), res.IsError
	}
}

// toolSession connects to one thread's MCP server over HTTP, as its agent does.
func toolSession(t *testing.T, a *App, w *workspace.Workspace, thread string) *mcp.ClientSession {
	t.Helper()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
		Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: a.mcp.URL(w, thread)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}
