package app

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
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
	a := newTestAppWith(t, quietCLI(), mcptest.Links("https://go.dev/made-up"))
	if err := a.runner.Available(context.Background()); err != nil {
		t.Skip(err)
	}
	w, err := a.store.Create("Learn Go", "claude")
	if err != nil {
		t.Fatal(err)
	}

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
	out, isErr := a.caller(t, w, workspace.WriterThread("go.strings"))("create_task", mcptest.ReverseWordsTask("go.strings"))
	if isErr {
		t.Fatalf("create_task: %s", out)
	}
	created, _ := decode.JSON[createTaskOutput]([]byte(out))
	if !created.Published {
		t.Fatalf("task not published:\n%s", out)
	}

	st, _ := a.Workspaces.Workspace(w.ID)
	if st.Progress.Total != 2 || st.Progress.Passed != 0 || st.Progress.Objectives["go.strings"].Status != "in_progress" {
		t.Fatalf("progress before = %+v", st.Progress)
	}

	// The learner's starter code fails; the reference passes and counts.
	res, err := a.Tasks.RunTests(w.ID, created.TaskID, sandbox.Files{"solution.go": mcptest.GoStarter}, true)
	if err != nil || res.Status != sandbox.StatusFailed {
		t.Fatalf("starter submit = %v %+v", err, res)
	}
	res, err = a.Tasks.RunTests(w.ID, created.TaskID, sandbox.Files{"solution.go": mcptest.GoReference}, true)
	if err != nil || res.Status != sandbox.StatusPassed {
		t.Fatalf("reference submit = %v %+v", err, res)
	}
	st, _ = a.Workspaces.Workspace(w.ID)
	if st.Progress.Passed != 1 || st.Progress.Objectives["go.strings"].Status != "passed" {
		t.Fatalf("progress after = %+v", st.Progress)
	}

	// Learners can't see hidden tests or the reference.
	d, _ := a.Tasks.Task(w.ID, created.TaskID)
	if _, leaked := d.Tests["hidden_test.go"]; leaked || d.HiddenCount != 2 {
		t.Fatalf("task detail = tests %v hidden %d", d.Tests, d.HiddenCount)
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
	agents *threadAgents
	mcp    *mcp.Server
}

// caller calls one thread's MCP tools, the way that thread's agent would
// (mcptest.Caller).
func (a *testApp) caller(t *testing.T, w *workspace.Workspace, thread workspace.Thread) func(name string, args any) (string, bool) {
	t.Helper()
	return mcptest.Caller(t, a.mcp.URL(w, thread, a.agents))
}

// newTestApp is newTestAppWith an agent CLI whose sessions take every
// message and never answer, so no test starts a real agent by accident, and
// links that all open.
func newTestApp(t *testing.T) *testApp {
	t.Helper()
	return newTestAppWith(t, quietCLI(), mcptest.Links())
}

// quietCLI is an agentCLI whose sessions take every message and never answer.
func quietCLI() agentCLI {
	return startFunc(func(context.Context, string, agent.Options, func(agent.Event)) (agent.Session, error) {
		return &fakeSession{prompts: new([]string), mu: &sync.Mutex{}}, nil
	})
}

// newTestAppWith keeps a test's workspaces and settings in temp dirs, checks
// links with links, and starts agents with cli. (Setting XDG_DATA_HOME
// wouldn't work: adrg/xdg reads it once at startup.)
func newTestAppWith(t *testing.T, cli agentCLI, links *mcp.LinkChecker) *testApp {
	t.Helper()
	store := &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	settingsStore := settings.NewStore(filepath.Join(t.TempDir(), "settings.yaml"))
	runner := sandbox.New()
	s, err := newServices(store, settingsStore, runner, links, cli)
	if err != nil {
		t.Fatal(err)
	}
	return &testApp{Services: s, store: store, runner: runner, agents: s.Chat.agents, mcp: s.Chat.agents.mcp}
}

// startFunc is an agentCLI that starts sessions by calling itself.
type startFunc func(ctx context.Context, provider string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error)

func (f startFunc) Start(ctx context.Context, provider string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error) {
	return f(ctx, provider, opts, onEvent)
}

// sandboxApp is newTestApp for tests that need a container engine.
func sandboxApp(t *testing.T) *testApp {
	t.Helper()
	a := newTestApp(t)
	if err := a.runner.Available(context.Background()); err != nil {
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

// Each chat thread gets its own tutor session, MCP endpoint and role, and
// answers to a form go back to the thread that asked.
func TestThreadsGetOwnSessions(t *testing.T) {
	type started struct {
		opts    agent.Options
		prompts []string
	}
	var mu sync.Mutex
	sessions := map[string]*started{} // by MCP URL
	a := newTestAppWith(t, startFunc(func(_ context.Context, _ string, opts agent.Options, _ func(agent.Event)) (agent.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		s := &started{opts: opts}
		sessions[opts.MCPURL] = s
		return &fakeSession{prompts: &s.prompts, mu: &mu}, nil
	}), mcptest.Links())
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Chat.SendMessage(w.ID, "hello plan", workspace.PlanThread.String()); err != nil {
		t.Fatal(err)
	}
	if err := a.Chat.SendMessage(w.ID, "hello task", "0001-some-task"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetQuestions(&workspace.Questions{Thread: "0001-some-task", Questions: []workspace.Question{{ID: "q", Question: "Which?"}}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Chat.AnswerQuestions(w.ID, map[string]string{"q": "this one"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		plan, task := sessions[a.mcp.URL(w, workspace.PlanThread, a.agents)], sessions[a.mcp.URL(w, workspace.TaskThread("0001-some-task"), a.agents)]
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
	plan, _ := a.Chat.Chat(w.ID, workspace.PlanThread.String())
	task, _ := a.Chat.Chat(w.ID, "0001-some-task")
	if len(plan) != 1 || len(task) != 2 {
		t.Fatalf("plan chat %d items, task chat %d items", len(plan), len(task))
	}
}
