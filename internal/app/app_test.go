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
	a := newTestAppWith(t, mcptest.Links("https://go.dev/made-up"), threadstest.Quiet())
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

	// The learner's starter code fails; the reference passes and counts.
	res, err := a.Tasks.RunTests(w.ID, created.TaskID, sandbox.Files{"solution.go": mcptest.GoStarter}, true)
	if err != nil || res.Status != sandbox.StatusFailed {
		t.Fatalf("starter submit = %v %+v", err, res)
	}
	res, err = a.Tasks.RunTests(w.ID, created.TaskID, sandbox.Files{"solution.go": mcptest.GoReference}, true)
	if err != nil || res.Status != sandbox.StatusPassed {
		t.Fatalf("reference submit = %v %+v", err, res)
	}
	st, _ := a.Workspaces.Workspace(w.ID)
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
	agents *threads.Agents
	mcp    *mcp.Server
}

// caller calls one thread's MCP tools, the way that thread's agent would
// (mcptest.Caller).
func (a *testApp) caller(t *testing.T, w *workspace.Workspace, thread workspace.Thread) func(name string, args any) (string, bool) {
	t.Helper()
	return mcptest.Caller(t, a.mcp.URL(w, thread, a.agents))
}

// newTestApp is newTestAppWith links that all open.
func newTestApp(t *testing.T) *testApp {
	t.Helper()
	return newTestAppWith(t, mcptest.Links(), threadstest.Quiet())
}

// newTestAppWith keeps a test's workspaces and settings in temp dirs, checks
// links with links, and starts agents on cli. newTestApp's agents take every
// message and never answer, so no test starts a real agent by accident.
// (Setting XDG_DATA_HOME wouldn't work: adrg/xdg reads it once at startup.)
func newTestAppWith(t *testing.T, links *mcp.LinkChecker, cli threads.CLI) *testApp {
	t.Helper()
	store := &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	settingsStore := settings.NewStore(filepath.Join(t.TempDir(), "settings.yaml"))
	runner := sandbox.New()
	tools, err := mcp.Start(runner, links)
	if err != nil {
		t.Fatal(err)
	}
	agents := threads.NewAgents(settingsStore, tools, cli)
	return &testApp{Services: newServices(store, settingsStore, runner, agents), store: store, runner: runner, agents: agents, mcp: tools}
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
