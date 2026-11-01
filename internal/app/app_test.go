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
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestToolsEndToEnd drives the MCP tools the way the coach would, without an
// AI: propose → approve → create_task (quality gate) → learner submits.
// Set TUTORTOP_RUNNER=local to run without Docker.
func TestToolsEndToEnd(t *testing.T) {
	a := newTestApp(t, nil)
	var prompts []string
	a.startAgent = func(_ context.Context, _ string, _ agent.Options, _ func(agent.Event)) (agent.Session, error) {
		return &fakeSession{prompts: &prompts}, nil
	}
	if err := a.runner.Check(context.Background()); err != nil {
		t.Skip(err)
	}
	w, err := a.store.Create("Learn Go", "claude")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: a.mcp.URL(w, workspace.PlanThread)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	callTool := func(name string, args any) (string, bool) {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
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
	call := func(name string, args any) string {
		t.Helper()
		out, isErr := callTool(name, args)
		if isErr {
			t.Fatalf("%s returned an error: %s", name, out)
		}
		return out
	}

	// A plan with an unsupported check kind is rejected with a reason.
	bad, isErr := callTool("propose_curriculum", map[string]any{
		"language": "go", "modules": []any{map[string]any{"id": "basics", "title": "Basics", "references": []any{map[string]any{"title": "Docs", "url": "https://go.dev/doc/"}},
			"objectives": []any{map[string]any{"id": "go.style", "name": "Style", "title": "Idiomatic style", "verify": "ai-review"}}}},
	})
	if !isErr || !strings.Contains(bad, "verify must be 'tests'") {
		t.Fatalf("expected rejection, got %s", bad)
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

	spec := map[string]any{
		"objectiveId": "go.strings",
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
	// Planning chats can't publish tasks; the objective's writer can.
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "create_task" || tool.Name == "create_lesson" {
			t.Fatalf("the planning chat has %s", tool.Name)
		}
	}
	write := toolCaller(t, a, w, workspace.WriterThread("go.strings"))
	if out, isErr := toolCaller(t, a, w, workspace.WriterThread("go.maps"))("create_task", spec); !isErr || !strings.Contains(out, "go.maps") {
		t.Fatalf("a writer published for another objective: %s", out)
	}
	out, isErr := write("create_task", spec)
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

// newTestApp keeps a test's workspaces in temp dirs. (Setting XDG_DATA_HOME
// wouldn't work: adrg/xdg reads it once at startup.)
func newTestApp(t *testing.T, emit Emitter) *App {
	t.Helper()
	a, err := New(emit)
	if err != nil {
		t.Fatal(err)
	}
	a.store = &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	a.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	a.linkCheck = func(context.Context, []string) []string { return nil }
	// Never start a real agent from a test; tests that watch the agent replace this.
	a.startAgent = func(context.Context, string, agent.Options, func(agent.Event)) (agent.Session, error) {
		return &fakeSession{prompts: new([]string), mu: &sync.Mutex{}}, nil
	}
	return a
}

// toolCaller connects to one thread's MCP tools, the way that thread's agent
// would, and returns a function that calls a tool and reports its text and
// whether it was an error.
func toolCaller(t *testing.T, a *App, w *workspace.Workspace, thread string) func(name string, args any) (string, bool) {
	t.Helper()
	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{Endpoint: a.mcp.URL(w, thread)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return func(name string, args any) (string, bool) {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
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

type fakeSession struct {
	id      string
	prompts *[]string
	mu      *sync.Mutex // guards prompts when the test reads them concurrently
}

func (f *fakeSession) Prompt(_ context.Context, text string) error {
	if f.mu != nil {
		f.mu.Lock()
		defer f.mu.Unlock()
	}
	*f.prompts = append(*f.prompts, text)
	return nil
}
func (f *fakeSession) ID() string   { return f.id }
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

// Each chat thread gets its own coach session, MCP endpoint and role, and
// answers to a form go back to the thread that asked.
func TestThreadsGetOwnSessions(t *testing.T) {
	a := newTestApp(t, nil)
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
	if err := a.SendMessage(w.ID, "hello plan", workspace.PlanThread); err != nil {
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
		plan, task := sessions[a.mcp.URL(w, workspace.PlanThread)], sessions[a.mcp.URL(w, "0001-some-task")]
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
	plan, _ := a.Chat(w.ID, workspace.PlanThread)
	task, _ := a.Chat(w.ID, "0001-some-task")
	if len(plan) != 1 || len(task) != 2 {
		t.Fatalf("plan chat %d items, task chat %d items", len(plan), len(task))
	}
}
