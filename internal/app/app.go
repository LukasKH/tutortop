// Package app ties workspaces, the check runner, the MCP tools and the agent
// together, and exposes them to the frontend as a Wails service.
package app

import (
	"cmp"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/adrg/xdg"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type App struct {
	store  *workspace.Store
	runner *runner.Runner
	mcp    *mcpHost

	// startAgent is agent.Start; tests replace it.
	startAgent func(ctx context.Context, provider string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error)
	// linkCheck is checkLinks; tests replace it so they don't need the network.
	linkCheck func(ctx context.Context, urls []string) []string

	mu          sync.Mutex
	agents      map[threadKey]*threadAgent // each thread's running agent (agents.go)
	lastResults map[taskKey]runner.Result  // latest check per task, for tutor context
	servers     map[string]*languageServer // running language servers by id
	seq         uint64                     // numbers busy and check changes and snapshots in order (agents.go)

	settingsMu   sync.Mutex
	settingsPath string
	models       map[string]modelList // provider -> recently listed models
}

func New() (*App, error) {
	a := &App{runner: runner.New(), agents: map[threadKey]*threadAgent{}, lastResults: map[taskKey]runner.Result{},
		servers: map[string]*languageServer{}, startAgent: agent.Start, linkCheck: checkLinks,
		settingsPath: filepath.Join(xdg.ConfigHome, "tutortop", "settings.yaml"), models: map[string]modelList{},
		// Counting from the time in microseconds keeps the numbers rising
		// across a restart, for a page that outlives one in server mode.
		seq: uint64(time.Now().UnixMicro())}
	// The workspaces folder: TUTORTOP_HOME, else the one chosen in the
	// settings, else ~/Tutortop. It's fixed for the run: running agents open
	// their workspace by id.
	a.store = workspace.NewStore(cmp.Or(os.Getenv("TUTORTOP_HOME"), a.loadSettings().WorkspacesFolder, workspace.DefaultRoot()))
	h, err := startMCP(a)
	if err != nil {
		return nil, err
	}
	a.mcp = h
	return a, nil
}

// ServiceStartup is called by Wails as the app starts. It builds the sandbox
// images of the workspaces' plans in the background, so the first check in
// each is fast. Reading a workspace has no such side effect.
func (a *App) ServiceStartup(context.Context, application.ServiceOptions) error {
	go func() {
		list, _ := a.store.List()
		for _, s := range list {
			w, err := a.store.Open(s.ID)
			if err != nil {
				continue
			}
			if c, _ := w.Curriculum(); c != nil {
				a.prepare(w, c.Language)
			}
		}
	}()
	return nil
}

// ServiceShutdown is called by Wails on exit and stops all agent processes
// and language servers.
func (a *App) ServiceShutdown() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ta := range a.agents {
		a.end(ta)
	}
	for _, ls := range a.servers {
		ls.srv.Close()
	}
	return nil
}

type ProviderStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
}

type Environment struct {
	Providers   []ProviderStatus `json:"providers"`
	SandboxOK   bool             `json:"sandboxOk"`
	SandboxHint string           `json:"sandboxHint,omitempty"`
	// Root is the workspaces folder in use. A folder chosen in the settings
	// is used from the next start; TUTORTOP_HOME overrides it (RootFromEnv).
	Root        string `json:"root"`
	DefaultRoot string `json:"defaultRoot"`
	RootFromEnv bool   `json:"rootFromEnv"`
}

func (a *App) Environment() Environment {
	env := Environment{Root: a.store.Root, DefaultRoot: workspace.DefaultRoot(), RootFromEnv: os.Getenv("TUTORTOP_HOME") != ""}
	for _, p := range agent.Providers() {
		path, err := exec.LookPath(p.Binary)
		env.Providers = append(env.Providers, ProviderStatus{ID: p.ID, Name: p.Name, Installed: err == nil, Path: path})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.runner.Check(ctx); err != nil {
		env.SandboxHint = err.Error()
	} else {
		env.SandboxOK = true
	}
	return env
}
