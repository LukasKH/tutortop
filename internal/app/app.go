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
	agents *threadAgents // each thread's running agent (agents.go)
	links  *linkChecker

	settings *settingsStore
	models   *modelCache

	mu      sync.Mutex
	servers map[string]*languageServer // running language servers by id
}

func New() (*App, error) {
	settings := newSettingsStore(filepath.Join(xdg.ConfigHome, "tutortop", "settings.yaml"))
	// The workspaces folder: TUTORTOP_HOME, else the one chosen in the
	// settings, else ~/Tutortop. It's fixed for the run: running agents open
	// their workspace by id.
	store := workspace.NewStore(cmp.Or(os.Getenv("TUTORTOP_HOME"), settings.Load().WorkspacesFolder, workspace.DefaultRoot()))
	return newApp(store, settings, installedCLI{})
}

// newApp builds the app on the workspaces in store, the settings and the
// agent CLI it starts. Tests pass their own.
func newApp(store *workspace.Store, settings *settingsStore, cli agentCLI) (*App, error) {
	a := &App{store: store, settings: settings, runner: runner.New(), links: newLinkChecker(), models: newModelCache(), servers: map[string]*languageServer{}}
	h, err := startMCP(a)
	if err != nil {
		return nil, err
	}
	a.mcp = h
	a.agents = newThreadAgents(settings, h, cli)
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
				a.prepareImage(w, c.Language)
			}
		}
	}()
	return nil
}

// ServiceShutdown is called by Wails on exit and stops all agent processes
// and language servers.
func (a *App) ServiceShutdown() error {
	a.agents.endAll()
	a.mu.Lock()
	defer a.mu.Unlock()
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
	if err := a.runner.Available(ctx); err != nil {
		env.SandboxHint = err.Error()
	} else {
		env.SandboxOK = true
	}
	return env
}
