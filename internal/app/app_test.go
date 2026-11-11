package app

import (
	"path/filepath"
	"testing"

	"github.com/LukasKH/tutortop/internal/agent/threads"
	"github.com/LukasKH/tutortop/internal/agent/threads/threadstest"
	"github.com/LukasKH/tutortop/internal/mcp"
	"github.com/LukasKH/tutortop/internal/mcp/mcptest"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/settings"
	"github.com/LukasKH/tutortop/internal/workspace"
)

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
func (a *testApp) caller(t testing.TB, w *workspace.Workspace, thread workspace.Thread) func(name string, args any) (string, bool) {
	t.Helper()
	return mcptest.Caller(t, a.mcp.URL(w, thread, a.agents))
}

// appDirs are where an app keeps its workspaces and settings. Opening a
// second app on the same dirs is a restart.
type appDirs struct {
	root, private, settings string
}

func tempDirs(t testing.TB) appDirs {
	return appDirs{root: t.TempDir(), private: t.TempDir(), settings: filepath.Join(t.TempDir(), "settings.yaml")}
}

// newTestApp keeps a test's workspaces and settings in temp dirs. Its agents
// take every message and never answer, so no test starts a real agent by
// accident.
func newTestApp(t testing.TB) *testApp {
	t.Helper()
	return newTestAppWith(t, tempDirs(t), threadstest.Quiet())
}

// missingPage is the one page test apps can't open.
const missingPage = "https://go.dev/made-up"

// newTestAppWith builds the app on dirs, with links that all open but
// missingPage, and thread agents on cli. (Setting XDG_DATA_HOME wouldn't work: adrg/xdg reads it
// once at startup.)
func newTestAppWith(t testing.TB, dirs appDirs, cli threads.CLI) *testApp {
	t.Helper()
	store := &workspace.Store{Root: dirs.root, PrivateRoot: dirs.private}
	settingsStore := settings.NewStore(dirs.settings)
	runner := sandbox.New()
	tools, err := mcp.Start(runner, mcptest.Links(missingPage))
	if err != nil {
		t.Fatal(err)
	}
	agents := threads.NewAgents(settingsStore, tools, cli)
	a := &testApp{
		Services: newServices(store, settingsStore, runner, agents),
		store:    store,
		runner:   runner,
		agents:   agents,
		mcp:      tools,
	}
	// Ends the thread agents as the app does when it quits.
	t.Cleanup(func() { _ = a.Chat.ServiceShutdown() })
	return a
}

// needSandbox fails the test without Docker or Podman, as the sandbox tests do.
func needSandbox(t testing.TB, a *testApp) {
	t.Helper()
	if err := a.runner.Available(t.Context()); err != nil {
		t.Fatalf("this test needs Docker or Podman: %v", err)
	}
}
