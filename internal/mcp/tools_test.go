package mcp_test

import (
	"context"
	"maps"
	"slices"
	"testing"

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
	lesson := workspace.LessonSpec{
		ObjectiveID: "go.strings", Title: "Strings", Summary: "Work with strings.", Body: "Strings are bytes ([spec](https://go.dev/ref/spec)).\n\n```go\ns := \"hi\"\n```",
		PrimarySource: workspace.Source{Title: "Go", URL: "https://go.dev/"},
		Sources:       []workspace.Source{{Title: "Go spec", URL: "https://go.dev/ref/spec"}},
		Quiz: []workspace.QuizQuestion{
			{Question: "A?", Options: []string{"a", "b", "c"}, Answer: 0, Explanation: "A."},
			{Question: "B?", Options: []string{"a", "b", "c"}, Answer: 1, Explanation: "B."},
			{Question: "C?", Options: []string{"a", "b", "c"}, Answer: 2, Explanation: "C."},
		},
	}
	for tool, spec := range map[string]any{"create_lesson": lesson, "create_task": noHiddenTestsTask("go.strings")} {
		if out, isErr := write(tool, spec); !isErr {
			t.Errorf("%s for another objective: %s", tool, out)
		}
	}
	lessons, _ := w.Lessons()
	tasks, _ := w.Tasks()
	if len(lessons) != 0 || len(tasks) != 0 {
		t.Fatalf("lessons = %+v, tasks = %+v", lessons, tasks)
	}
}

// create_lesson checks that every URL in sources opens, not just body links.
func TestCreateLessonChecksSourceLinks(t *testing.T) {
	runner := sandbox.New()
	server, err := mcp.Start(runner, mcptest.Links("https://go.dev/broken"))
	if err != nil {
		t.Fatal(err)
	}
	tt := &tools{store: &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}, runner: runner, server: server}
	w := tt.goWorkspace(t)
	write := tt.caller(t, w, workspace.WriterThread("go.strings"))
	lesson := workspace.LessonSpec{
		ObjectiveID: "go.strings", Title: "Strings", Summary: "Work with strings.", Body: "Strings are bytes ([spec](https://go.dev/ref/spec)).\n\n```go\ns := \"hi\"\n```",
		PrimarySource: workspace.Source{Title: "Go", URL: "https://go.dev/"},
		Sources:       []workspace.Source{{Title: "Broken", URL: "https://go.dev/broken"}},
		Quiz: []workspace.QuizQuestion{
			{Question: "A?", Options: []string{"a", "b", "c"}, Answer: 0, Explanation: "A."},
			{Question: "B?", Options: []string{"a", "b", "c"}, Answer: 1, Explanation: "B."},
			{Question: "C?", Options: []string{"a", "b", "c"}, Answer: 2, Explanation: "C."},
		},
	}
	if out, isErr := write("create_lesson", lesson); !isErr {
		t.Fatalf("accepted lesson with broken source url: %s", out)
	}
	if lessons, _ := w.Lessons(); len(lessons) != 0 {
		t.Fatalf("saved lesson with broken source url: %+v", lessons)
	}
}

// TestSetToolchainEnablesALanguage sets up JavaScript the way the tutor would.
func TestSetToolchainEnablesALanguage(t *testing.T) {
	tt := newTools(t)
	if err := tt.runner.Available(context.Background()); err != nil {
		t.Fatalf("this test needs Docker or Podman: %v", err)
	}
	w := tt.create(t, "Learn JavaScript")
	call := tt.caller(t, w, workspace.PlanThread)

	tests := map[string]string{"add.test.mjs": "import { test } from 'node:test';\nimport assert from 'node:assert';\nimport { add } from './add.mjs';\n\n" +
		"test('adds', () => { assert.strictEqual(add(1, 2), 3); });\n"}
	toolchain := map[string]any{
		"language": "javascript", "name": "JavaScript", "editorLanguage": "javascript",
		"dockerfile":  "FROM node:24-alpine",
		"testCommand": "node --test --test-reporter=junit --test-reporter-destination=/tmp/junit.xml",
		"testFiles":   "*.test.mjs",
	}
	setup := func(toolchain map[string]any, solution string) (string, bool) {
		return call("set_toolchain", map[string]any{"toolchain": toolchain, "example": map[string]any{
			"solution": map[string]string{"add.mjs": solution}, "tests": tests,
		}})
	}
	reference := "export function add(a, b) { return a + b; }\n"

	// An example that fails proves nothing, so it's sent back.
	if out, isErr := setup(toolchain, "export function add(a, b) { return a - b; }\n"); !isErr {
		t.Fatalf("a failing example was accepted: %s", out)
	}
	if out, isErr := setup(toolchain, reference); isErr {
		t.Fatalf("set_toolchain: %s", out)
	}

	// A language server that doesn't answer doesn't fail the setup; the
	// language is saved without one.
	broken := maps.Clone(toolchain)
	broken["languageServer"] = []string{"false"}
	if out, isErr := setup(broken, reference); isErr {
		t.Fatalf("set_toolchain with a broken language server: %s", out)
	}
	if specs := w.ToolchainSpecs(); len(specs) != 1 || len(specs[0].LanguageServer) != 0 {
		t.Fatalf("saved = %+v", specs)
	}
}

// noHiddenTestsTask is a well-formed Go task for objective whose quality
// gate fails before running anything: it has no hidden tests.
func noHiddenTestsTask(objective string) workspace.TaskSpec {
	return workspace.TaskSpec{
		ObjectiveID: objective,
		Title:       "Add",
		Statement: workspace.Statement{
			Task: "Write Add.", Context: "Sums.", Signature: "func Add(a, b int) int",
			Requirements: []string{"Return a+b."},
			Examples:     []workspace.Example{{Input: "Add(1, 2)", Output: "3"}, {Input: "Add(0, 0)", Output: "0"}},
		},
		Starter:     sandbox.Files{"add.go": "package add\n"},
		Reference:   sandbox.Files{"add.go": "package add\n"},
		Tests:       sandbox.Files{"add_test.go": "package add\n"},
		HiddenTests: sandbox.Files{},
	}
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

func (noChecks) StartCheck(string, events.SandboxCheck) mcp.Check { return func(bool) {} }

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
