package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/agent/threads"
	"github.com/LukasKH/tutortop/internal/agent/threads/threadstest"
	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/LukasKH/tutortop/internal/mcp"
	"github.com/LukasKH/tutortop/internal/mcp/mcptest"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/settings"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// TestToolsEndToEnd drives the MCP tools the way the tutor would, without an
// AI: propose → approve → create_task (quality gate) → learner submits.
// Set TUTORTOP_RUNNER=local to run without Docker.
func TestToolsEndToEnd(t *testing.T) {
	a := newTestApp(t, mcptest.Links("https://go.dev/made-up"))
	if err := a.runner.Available(context.Background()); err != nil {
		t.Skip(err)
	}
	w, err := a.store.Create("Learn Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	// The learner's runs below leave a cache volume in Docker.
	t.Cleanup(func() { _ = a.runner.RemoveCache(context.Background(), w.ID) })

	callTool := a.caller(t, w, workspace.PlanThread)
	call := func(name string, args any) string {
		t.Helper()
		out, isErr := callTool(name, args)
		if isErr {
			t.Fatalf("%s returned an error: %s", name, out)
		}
		return out
	}

	// A reference that doesn't open is sent back.
	if out, isErr := callTool("propose_curriculum", map[string]any{
		"language": "go", "modules": []any{map[string]any{"id": "basics", "title": "Basics",
			"references": []any{map[string]any{"title": "Made up", "url": "https://go.dev/made-up"}},
			"objectives": []any{map[string]any{"id": "go.strings", "name": "Strings", "title": "Strings"}}}},
	}); !isErr || !strings.Contains(out, "made-up returns 404") {
		t.Fatalf("a broken reference was accepted: %s", out)
	}

	call("propose_curriculum", map[string]any{
		"language": "go", "taskDefaults": map[string]any{"size": "small"},
		"modules": []any{map[string]any{"id": "basics", "title": "Basics", "references": []any{map[string]any{"title": "Docs", "url": "https://go.dev/doc/"}}, "objectives": []any{
			map[string]any{"id": "go.strings", "name": "Strings", "title": "Strings and slices", "verify": "tests"},
			map[string]any{"id": "go.maps", "name": "Maps", "title": "Maps", "verify": "tests"},
		}}},
	})
	if err := a.Workspaces.ApproveProposal(w.ID); err != nil {
		t.Fatal(err)
	}

	// The objective's writer publishes the task.
	out, isErr := a.caller(t, w, workspace.WriterThread("go.strings"))("create_task", reverseWordsTask("go.strings"))
	if isErr {
		t.Fatalf("create_task: %s", out)
	}
	created, _ := decode.JSON[createTaskOutput]([]byte(out))
	if !created.Published {
		t.Fatalf("task not published:\n%s", out)
	}

	// The learner's starter code fails; the reference passes and counts.
	res, err := a.Tasks.RunTests(w.ID, created.TaskID, sandbox.Files{"solution.go": goStarter}, true)
	if err != nil || res.Status != sandbox.StatusFailed {
		t.Fatalf("starter submit = %v %+v", err, res)
	}
	res, err = a.Tasks.RunTests(w.ID, created.TaskID, sandbox.Files{"solution.go": goReference}, true)
	if err != nil || res.Status != sandbox.StatusPassed {
		t.Fatalf("reference submit = %v %+v", err, res)
	}
	st, _ := a.Workspaces.Workspace(w.ID)
	if st.Progress.Passed != 1 || st.Progress.Objectives["go.strings"].Status != "passed" {
		t.Fatalf("progress after = %+v", st.Progress)
	}
}

// createTaskOutput is the part of create_task's result the tests read.
type createTaskOutput struct {
	Published bool   `json:"published"`
	TaskID    string `json:"taskId"`
}

// testApp is the app's services, wired by newServices as New wires them,
// and the shared pieces tests look into.
type testApp struct {
	*Services
	store  *workspace.Store
	runner *sandbox.Runner
	agents *threads.Agents
	mcp    *mcp.Server
}

// caller calls one thread's MCP tools, the way that thread's agent would
// (mcptest.Caller).
func (a *testApp) caller(t *testing.T, w *workspace.Workspace, thread workspace.Thread) func(name string, args any) (string, bool) {
	t.Helper()
	return mcptest.Caller(t, a.mcp.URL(w, thread, a.agents))
}

// newTestApp keeps a test's workspaces and settings in temp dirs and checks
// links with links. Its agents take every message and never answer, so no
// test starts a real agent by accident. (Setting XDG_DATA_HOME wouldn't
// work: adrg/xdg reads it once at startup.)
func newTestApp(t *testing.T, links *mcp.LinkChecker) *testApp {
	t.Helper()
	store := &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	settingsStore := settings.NewStore(filepath.Join(t.TempDir(), "settings.yaml"))
	runner := sandbox.New()
	tools, err := mcp.Start(runner, links)
	if err != nil {
		t.Fatal(err)
	}
	agents := threads.NewAgents(settingsStore, tools, threadstest.Quiet())
	return &testApp{Services: newServices(store, settingsStore, runner, agents), store: store, runner: runner, agents: agents, mcp: tools}
}

// reverseWordsTask is create_task's input for a Go task for objective that
// passes the quality gate.
func reverseWordsTask(objective string) map[string]any {
	return map[string]any{
		"objectiveId": objective,
		"title":       "Reverse words",
		"statement": map[string]any{
			"task":         "Write ReverseWords, which returns the words of a sentence in reverse order.",
			"context":      "Command-line tools often reorder user input.",
			"signature":    "func ReverseWords(s string) string",
			"requirements": []string{"Words are separated by runs of spaces.", "Join the result with single spaces."},
			"examples": []any{
				map[string]any{"input": `ReverseWords("hello world")`, "output": `"world hello"`},
				map[string]any{"input": `ReverseWords("  a   b ")`, "output": `"b a"`, "explanation": "Extra spaces don't make empty words."},
			},
		},
		"starter":     map[string]string{"solution.go": goStarter},
		"reference":   map[string]string{"solution.go": goReference},
		"tests":       map[string]string{"visible_test.go": goVisible},
		"hiddenTests": map[string]string{"hidden_test.go": goHidden},
		"mutants": []any{
			map[string]any{"name": "split", "description": "keeps empty words", "files": map[string]string{"solution.go": strings.Replace(goReference, "strings.Fields(s)", `strings.Split(s, " ")`, 1)}},
			map[string]any{"name": "no-reverse", "description": "forgets to reverse", "files": map[string]string{"solution.go": "package solution\n\nimport \"strings\"\n\nfunc ReverseWords(s string) string { return strings.Join(strings.Fields(s), \" \") }\n"}},
		},
	}
}

// goReference is reverseWordsTask's reference solution, which passes every
// test.
const goReference = `package solution

import "strings"

func ReverseWords(s string) string {
	words := strings.Fields(s)
	for i, j := 0, len(words)-1; i < j; i, j = i+1, j-1 {
		words[i], words[j] = words[j], words[i]
	}
	return strings.Join(words, " ")
}
`

// goStarter is reverseWordsTask's starter code, which fails the tests.
const goStarter = "package solution\n\nfunc ReverseWords(s string) string {\n\treturn \"\"\n}\n"

const goVisible = `package solution

import "testing"

func TestBasic(t *testing.T) {
	if got := ReverseWords("hello world"); got != "world hello" {
		t.Fatalf("got %q", got)
	}
}
`

const goHidden = `package solution

import "testing"

func TestExtraSpaces(t *testing.T) {
	if got := ReverseWords("  a   b  "); got != "b a" {
		t.Fatalf("got %q", got)
	}
}

func TestEmpty(t *testing.T) {
	if got := ReverseWords(""); got != "" {
		t.Fatalf("got %q", got)
	}
}
`
