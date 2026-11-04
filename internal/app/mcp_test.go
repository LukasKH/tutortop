package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/LukasKH/tutortop/internal/runner"
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
	for thread, want := range map[workspace.Thread][]string{
		workspace.PlanThread:                   chat,
		workspace.TaskThread("0001-some-task"): chat,
		workspace.LessonThread("0001-x"):       chat,
		workspace.WriterThread("go.errors"):    writer,
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

// Our own tools show in the chat as their mcpTools entry says, whatever the
// agent titles them; other servers' tools aren't ours to hide.
func TestOwnToolsShowInTheChatAsListed(t *testing.T) {
	call := func(server, tool string) agent.Tool {
		return agent.Tool{Kind: agent.ToolOther, Title: "Called " + tool, MCPServer: server, MCPTool: tool}
	}
	if label, ok := toolLabel(call(mcpName, "ask_user")); ok {
		t.Errorf("ask_user shows as %q", label)
	}
	if label, ok := toolLabel(call(mcpName, "set_toolchain")); !ok || label != "tutortop · set_toolchain" {
		t.Errorf("set_toolchain shows as %q, %v", label, ok)
	}
	if _, ok := toolLabel(call("other", "ask_user")); !ok {
		t.Error("another server's ask_user is hidden")
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

// Struct tags can't be computed, so they name the built-in languages by
// hand. A tag that names two of them is listing them, and must name them
// all, so a new built-in language fails this until every list names it. The
// prompts fill the list in ({{builtins}}), but each language's conventions
// in the shared prompt are written by hand.
func TestWrittenOutLanguagesMatchTheBuiltins(t *testing.T) {
	// Where -> tag, from the packages whose types the agent reads as tool
	// schemas.
	tags := map[string]string{}
	tag := regexp.MustCompile(`jsonschema:"((?:[^"\\]|\\.)*)"`)
	sources, _ := filepath.Glob("*.go")
	for _, dir := range []string{"../workspace", "../runner"} {
		more, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		sources = append(sources, more...)
	}
	for _, file := range sources {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			for _, m := range tag.FindAllStringSubmatch(line, -1) {
				tags[fmt.Sprintf("%s:%d", file, i+1)] = m[1]
			}
		}
	}

	type builtin struct {
		lang string
		name *regexp.Regexp // its id or its name as a word
	}
	var builtins []builtin
	for _, lang := range runner.BuiltinLanguages() {
		tc, _ := runner.Builtin(lang)
		builtins = append(builtins, builtin{lang, regexp.MustCompile(`\b(` + regexp.QuoteMeta(lang) + `|` + regexp.QuoteMeta(tc.Name) + `)\b`)})
		if !strings.Contains(sharedPrompt, "\n- **"+tc.Name+" ") {
			t.Errorf("shared.md has no conventions for %s, as a line starting \"- **%s <version>**: \"", lang, tc.Name)
		}
	}
	for where, text := range tags {
		var named, missing []string
		for _, b := range builtins {
			if b.name.MatchString(text) {
				named = append(named, b.lang)
			} else {
				missing = append(missing, b.lang)
			}
		}
		if len(named) >= 2 && len(missing) > 0 {
			t.Errorf("%s names %v but not %v: %q", where, named, missing, text)
		}
	}
}

// Every placeholder in the prompts is filled, for every kind of thread.
func TestInstructionsFillEveryPlaceholder(t *testing.T) {
	a := newTestApp(t)
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	for _, thread := range []workspace.Thread{workspace.PlanThread, workspace.TaskThread("0001-x"), workspace.LessonThread("0001-x"), workspace.WriterThread("go.x")} {
		if text := instructions(w, thread); strings.Contains(text, "{{") {
			i := strings.Index(text, "{{")
			t.Errorf("%s: unfilled placeholder at %q", thread, text[i:min(len(text), i+20)])
		}
	}
}

// The tutor uses a built-in language as it is, before any image is built.
func TestSetToolchainRefusesABuiltin(t *testing.T) {
	a := newTestApp(t)
	w, err := a.store.Create("Python", "claude")
	if err != nil {
		t.Fatal(err)
	}
	out, isErr := toolCaller(t, a, w, workspace.PlanThread)("set_toolchain", map[string]any{
		"toolchain": map[string]any{"language": "python", "name": "Python", "editorLanguage": "python", "dockerfile": "FROM python:3.14", "testCommand": "pytest", "testFiles": "test_*.py"},
		"example":   map[string]any{"solution": map[string]string{"a.py": ""}, "tests": map[string]string{"test_a.py": ""}},
	})
	if !isErr || !strings.Contains(out, "python is built in") || len(w.ToolchainSpecs()) != 0 {
		t.Fatalf("set_toolchain for python: %s", out)
	}
}

// ask_user takes 2-4 questions, as its description and the tutor's prompt say.
func TestAskUserTakesTwoToFourQuestions(t *testing.T) {
	a := newTestApp(t)
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	ask := toolCaller(t, a, w, workspace.TaskThread("0001-some-task"))
	questions := func(n int) map[string]any {
		qs := []any{}
		for range n {
			qs = append(qs, map[string]any{"id": "q", "question": "Which?"})
		}
		return map[string]any{"questions": qs}
	}
	for _, n := range []int{0, 1, 5} {
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
	if err := a.runner.Available(context.Background()); err != nil {
		t.Skip(err)
	}
	w := goWorkspace(t, a)
	spec := reverseWordsTask("go.strings")
	spec["mutants"] = []any{
		map[string]any{"name": "same", "description": "the reference itself", "files": map[string]string{"solution.go": goReference}},
		map[string]any{"name": "same-again", "description": "the reference again", "files": map[string]string{"solution.go": goReference}},
	}
	out, isErr := toolCaller(t, a, w, workspace.WriterThread("go.strings"))("create_task", spec)
	res, err := decode.JSON[createTaskOutput]([]byte(out))
	if err != nil {
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
	planned(t, w, "go.strings", "go.maps")
	return w
}

// toolCaller connects to one thread's MCP tools, the way that thread's agent
// would, and returns a function that calls a tool and reports its text and
// whether it was an error.
func toolCaller(t *testing.T, a *App, w *workspace.Workspace, thread workspace.Thread) func(name string, args any) (string, bool) {
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
func toolSession(t *testing.T, a *App, w *workspace.Workspace, thread workspace.Thread) *mcp.ClientSession {
	t.Helper()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
		Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: a.mcp.URL(w, thread)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}
