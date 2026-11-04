package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// TestToolsEndToEnd drives the MCP tools the way the tutor would, without an
// AI: propose → approve → create_task (quality gate) → learner submits.
// Set TUTORTOP_RUNNER=local to run without Docker.
func TestToolsEndToEnd(t *testing.T) {
	a := newTestApp(t)
	if err := a.runner.Check(context.Background()); err != nil {
		t.Skip(err)
	}
	w, err := a.store.Create("Learn Go", "claude")
	if err != nil {
		t.Fatal(err)
	}

	callTool := toolCaller(t, a, w, workspace.PlanThread)
	call := func(name string, args any) string {
		t.Helper()
		out, isErr := callTool(name, args)
		if isErr {
			t.Fatalf("%s returned an error: %s", name, out)
		}
		return out
	}

	// A reference that doesn't open is sent back.
	a.linkCheck = func(_ context.Context, urls []string) []string { return []string{urls[0] + " returns 404 Not Found"} }
	if out, isErr := callTool("propose_curriculum", map[string]any{
		"language": "go", "modules": []any{map[string]any{"id": "basics", "title": "Basics",
			"references": []any{map[string]any{"title": "Made up", "url": "https://go.dev/made-up"}},
			"objectives": []any{map[string]any{"id": "go.strings", "name": "Strings", "title": "Strings"}}}},
	}); !isErr || !strings.Contains(out, "made-up returns 404") {
		t.Fatalf("a broken reference was accepted: %s", out)
	}
	a.linkCheck = func(context.Context, []string) []string { return nil }

	call("propose_curriculum", map[string]any{
		"language": "go", "taskDefaults": map[string]any{"size": "small"},
		"modules": []any{map[string]any{"id": "basics", "title": "Basics", "references": []any{map[string]any{"title": "Docs", "url": "https://go.dev/doc/"}}, "objectives": []any{
			map[string]any{"id": "go.strings", "name": "Strings", "title": "Strings and slices", "verify": "tests"},
			map[string]any{"id": "go.maps", "name": "Maps", "title": "Maps", "verify": "tests"},
		}}},
	})
	if err := a.ApproveProposal(w.ID); err != nil {
		t.Fatal(err)
	}

	// The objective's writer publishes the task.
	out, isErr := toolCaller(t, a, w, workspace.WriterThread("go.strings"))("create_task", reverseWordsTask("go.strings"))
	if isErr {
		t.Fatalf("create_task: %s", out)
	}
	var created createTaskOutput
	_ = json.Unmarshal([]byte(out), &created)
	if !created.Published {
		t.Fatalf("task not published:\n%s", out)
	}

	st, _ := a.Workspace(w.ID)
	if st.Progress.Total != 2 || st.Progress.Passed != 0 || st.Progress.Objectives["go.strings"].Status != "in_progress" {
		t.Fatalf("progress before = %+v", st.Progress)
	}

	// The learner's starter code fails; the reference passes and counts.
	res, err := a.RunChecks(w.ID, created.TaskID, runner.Files{"solution.go": goStarter}, true)
	if err != nil || res.Status != runner.StatusFailed {
		t.Fatalf("starter submit = %v %+v", err, res)
	}
	res, err = a.RunChecks(w.ID, created.TaskID, runner.Files{"solution.go": goReference}, true)
	if err != nil || res.Status != runner.StatusPassed {
		t.Fatalf("reference submit = %v %+v", err, res)
	}
	st, _ = a.Workspace(w.ID)
	if st.Progress.Passed != 1 || st.Progress.Objectives["go.strings"].Status != "passed" {
		t.Fatalf("progress after = %+v", st.Progress)
	}

	// Learners can't see hidden tests or the reference.
	d, _ := a.Task(w.ID, created.TaskID)
	if _, leaked := d.Tests["hidden_test.go"]; leaked || d.HiddenCount != 2 {
		t.Fatalf("task detail = tests %v hidden %d", d.Tests, d.HiddenCount)
	}
}

// reverseWordsTask is a Go task for objective that passes the quality gate.
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

// newTestApp keeps a test's workspaces in temp dirs. (Setting XDG_DATA_HOME
// wouldn't work: adrg/xdg reads it once at startup.)
func newTestApp(t *testing.T) *App {
	t.Helper()
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	a.store = &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	a.settingsPath = filepath.Join(t.TempDir(), "settings.yaml")
	a.linkCheck = func(context.Context, []string) []string { return nil }
	// Never start a real agent from a test; tests that watch the agent replace this.
	a.startAgent = func(context.Context, string, agent.Options, func(agent.Event)) (agent.Session, error) {
		return &fakeSession{prompts: new([]string), mu: &sync.Mutex{}}, nil
	}
	return a
}

// sandboxApp is newTestApp for tests that need a container engine.
func sandboxApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	if err := a.runner.Check(context.Background()); err != nil {
		t.Skip(err)
	}
	if a.runner.Engine == "local" {
		t.Skip("needs a container engine")
	}
	return a
}

type fakeSession struct {
	prompts *[]string
	mu      *sync.Mutex // guards prompts when the test reads them concurrently
}

func (f *fakeSession) Prompt(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	*f.prompts = append(*f.prompts, text)
	return nil
}
func (f *fakeSession) ID() string   { return "" }
func (f *fakeSession) Cancel()      {}
func (f *fakeSession) Close() error { return nil }

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

// Each chat thread gets its own tutor session, MCP endpoint and role, and
// answers to a form go back to the thread that asked.
func TestThreadsGetOwnSessions(t *testing.T) {
	a := newTestApp(t)
	type started struct {
		opts    agent.Options
		prompts []string
	}
	var mu sync.Mutex
	sessions := map[string]*started{} // by MCP URL
	a.startAgent = func(_ context.Context, _ string, opts agent.Options, _ func(agent.Event)) (agent.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		s := &started{opts: opts}
		sessions[opts.MCPURL] = s
		return &fakeSession{prompts: &s.prompts, mu: &mu}, nil
	}
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SendMessage(w.ID, "hello plan", workspace.PlanThread.String()); err != nil {
		t.Fatal(err)
	}
	if err := a.SendMessage(w.ID, "hello task", "0001-some-task"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetQuestions(&workspace.Questions{Thread: "0001-some-task", Questions: []workspace.Question{{ID: "q", Question: "Which?"}}}); err != nil {
		t.Fatal(err)
	}
	if err := a.AnswerQuestions(w.ID, map[string]string{"q": "this one"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		plan, task := sessions[a.mcp.URL(w, workspace.PlanThread)], sessions[a.mcp.URL(w, workspace.TaskThread("0001-some-task"))]
		done := plan != nil && task != nil && len(plan.prompts) == 1 && len(task.prompts) == 2
		mu.Unlock()
		if done {
			if !strings.Contains(plan.opts.Instructions, "planner") || !strings.Contains(task.opts.Instructions, "tutor for one task") {
				t.Fatal("threads didn't get their role instructions")
			}
			if !strings.Contains(task.prompts[1], "this one") {
				t.Fatalf("answers went elsewhere: %q", task.prompts)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sessions: %d", len(sessions))
		}
		time.Sleep(10 * time.Millisecond)
	}
	plan, _ := a.Chat(w.ID, workspace.PlanThread.String())
	task, _ := a.Chat(w.ID, "0001-some-task")
	if len(plan) != 1 || len(task) != 2 {
		t.Fatalf("plan chat %d items, task chat %d items", len(plan), len(task))
	}
}
