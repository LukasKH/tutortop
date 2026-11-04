package mcp_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/mcp"
	"github.com/LukasKH/tutortop/internal/mcp/mcptest"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// A thread's tools decide what its agent can change: chats talk with the
// learner and shape the plan, and only writers publish lessons and tasks.
func TestThreadsGetTheirKindsTools(t *testing.T) {
	tt := newTools(t)
	w := tt.create(t, "Go")
	chat := []string{"ask_user", "get_state", "propose_curriculum", "set_icon", "set_toolchain"}
	writer := []string{"create_lesson", "create_task", "get_state"}
	for thread, want := range map[workspace.Thread][]string{
		workspace.PlanThread:                   chat,
		workspace.TaskThread("0001-some-task"): chat,
		workspace.LessonThread("0001-x"):       chat,
		workspace.WriterThread("go.errors"):    writer,
	} {
		var got []string
		for tool, err := range mcptest.Session(t, tt.url(w, thread)).Tools(context.Background(), nil) {
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
	tt := newTools(t)
	w := tt.goWorkspace(t)
	write := tt.caller(t, w, workspace.WriterThread("go.maps"))
	if out, isErr := write("create_task", mcptest.ReverseWordsTask("go.strings")); !isErr || !strings.Contains(out, `not "go.strings"`) {
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
// all, so a new built-in language fails this until every list names it. (The
// prompts fill the list in; the threads' tests check their conventions.)
func TestWrittenOutLanguagesMatchTheBuiltins(t *testing.T) {
	// Where -> tag, from the packages whose types the agent reads as tool
	// schemas.
	tags := map[string]string{}
	tag := regexp.MustCompile(`jsonschema:"((?:[^"\\]|\\.)*)"`)
	sources, _ := filepath.Glob("*.go")
	for _, dir := range []string{"../workspace", "../sandbox"} {
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
	for _, lang := range sandbox.BuiltinLanguages() {
		tc, _ := sandbox.Builtin(lang)
		builtins = append(builtins, builtin{lang, regexp.MustCompile(`\b(` + regexp.QuoteMeta(lang) + `|` + regexp.QuoteMeta(tc.Name) + `)\b`)})
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

// The tutor uses a built-in language as it is, before any image is built.
func TestSetToolchainRefusesABuiltin(t *testing.T) {
	tt := newTools(t)
	w := tt.create(t, "Python")
	out, isErr := tt.caller(t, w, workspace.PlanThread)("set_toolchain", map[string]any{
		"toolchain": map[string]any{"language": "python", "name": "Python", "editorLanguage": "python", "dockerfile": "FROM python:3.14", "testCommand": "pytest", "testFiles": "test_*.py"},
		"example":   map[string]any{"solution": map[string]string{"a.py": ""}, "tests": map[string]string{"test_a.py": ""}},
	})
	if !isErr || !strings.Contains(out, "python is built in") || len(w.ToolchainSpecs()) != 0 {
		t.Fatalf("set_toolchain for python: %s", out)
	}
}

// ask_user takes 2-4 questions, as its description and the tutor's prompt say.
func TestAskUserTakesTwoToFourQuestions(t *testing.T) {
	tt := newTools(t)
	w := tt.create(t, "Go")
	ask := tt.caller(t, w, workspace.TaskThread("0001-some-task"))
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
	tt := newTools(t)
	if err := tt.runner.Available(context.Background()); err != nil {
		t.Skip(err)
	}
	w := tt.goWorkspace(t)
	spec := mcptest.ReverseWordsTask("go.strings")
	spec["mutants"] = []any{
		map[string]any{"name": "same", "description": "the reference itself", "files": map[string]string{"solution.go": mcptest.GoReference}},
		map[string]any{"name": "same-again", "description": "the reference again", "files": map[string]string{"solution.go": mcptest.GoReference}},
	}
	out, isErr := tt.caller(t, w, workspace.WriterThread("go.strings"))("create_task", spec)
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

// createTaskOutput is create_task's result, as the writer reads it.
type createTaskOutput struct {
	Published bool                `json:"published"`
	Gate      *sandbox.GateReport `json:"gate"`
	Report    string              `json:"report"`
}

// tools serves a test's workspaces' tools, with links that all open.
type tools struct {
	store  *workspace.Store
	runner *sandbox.Runner
	server *mcp.Server
}

func newTools(t *testing.T) *tools {
	t.Helper()
	runner := sandbox.New()
	server, err := mcp.Start(runner, mcptest.Links())
	if err != nil {
		t.Fatal(err)
	}
	return &tools{store: &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}, runner: runner, server: server}
}

func (tt *tools) create(t *testing.T, title string) *workspace.Workspace {
	t.Helper()
	w, err := tt.store.Create(title, "claude")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// goWorkspace is a workspace with an approved Go curriculum of two objectives.
func (tt *tools) goWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	w := tt.create(t, "Go")
	planned(t, w, "go.strings", "go.maps")
	return w
}

// url is a thread's MCP endpoint.
func (tt *tools) url(w *workspace.Workspace, thread workspace.Thread) string {
	return tt.server.URL(w, thread, noChecks{})
}

// caller calls a thread's tools, as mcptest.Caller does.
func (tt *tools) caller(t *testing.T, w *workspace.Workspace, thread workspace.Thread) func(name string, args any) (string, bool) {
	t.Helper()
	return mcptest.Caller(t, tt.url(w, thread))
}

// noChecks shows sandbox checks nowhere.
type noChecks struct{}

func (noChecks) Show(string, events.SandboxCheck) {}

// planned gives a workspace an approved curriculum with these objectives.
func planned(t *testing.T, w *workspace.Workspace, objectives ...string) {
	t.Helper()
	m := workspace.Module{ID: "m", Title: "M", References: []workspace.Source{{Title: "Go docs", URL: "https://go.dev/doc/"}}}
	for _, id := range objectives {
		m.Objectives = append(m.Objectives, workspace.Objective{ID: id, Name: id, Title: id})
	}
	if err := w.Propose(workspace.Curriculum{Language: "go", Modules: []workspace.Module{m}}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
}
