package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// engineCalls logs the commands the sandbox gives its container engine. A
// slowdown from an extra run, a rebuilt image or a lost build cache then
// shows as a count, which a timing on a shared runner can't show reliably.
type engineCalls struct {
	log  string
	read int
}

// watchEngine routes r's engine commands through a shim that logs them.
func watchEngine(t testing.TB, r *sandbox.Runner) *engineCalls {
	t.Helper()
	engine, err := exec.LookPath(string(r.Engine))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	e := &engineCalls{log: filepath.Join(dir, "calls")}
	shim := filepath.Join(dir, "engine")
	// One line per call: the subcommand, then "volume" if it mounts one. A
	// run's script can span lines, so the full arguments aren't logged.
	script := fmt.Sprintf(`#!/bin/sh
v=
for a in "$@"; do [ "$a" = -v ] && v=" volume"; done
printf '%%s%%s\n' "$1" "$v" >> '%s'
exec '%s' "$@"
`, e.log, engine)
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r.Engine = sandbox.Engine(shim)
	return e
}

// cacheMount is whether a check's container mounts the workspace's build cache.
type cacheMount bool

const (
	noCache    cacheMount = false
	buildCache cacheMount = true
)

// wantRuns fails unless the engine started n containers since the last
// check, each with mount, and did nothing else, such as checking or building
// an image.
func (e *engineCalls) wantRuns(t testing.TB, what string, n int, mount cacheMount) {
	t.Helper()
	b, err := os.ReadFile(e.log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	calls := strings.Split(strings.TrimSpace(string(b[e.read:])), "\n")
	e.read = len(b)
	runs := 0
	for _, call := range calls {
		if call == "" {
			continue
		}
		cmd, volume, _ := strings.Cut(call, " ")
		if cmd != "run" {
			t.Errorf("%s: the engine was asked to %s, want only container runs", what, cmd)
			continue
		}
		runs++
		if got := cacheMount(volume == "volume"); got != mount {
			t.Errorf("%s: a run mounts the build cache: %v, want %v", what, got, mount)
		}
	}
	if runs != n {
		t.Errorf("%s started %d containers, want %d", what, runs, n)
	}
}

// needSandbox fails the test without Docker or Podman, as the sandbox tests do.
func needSandbox(t testing.TB, a *testApp) {
	t.Helper()
	if err := a.runner.Available(t.Context()); err != nil {
		t.Fatalf("this test needs Docker or Podman: %v", err)
	}
}
