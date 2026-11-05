package settings

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Service serves the Settings screen: the app-wide settings, the models each
// agent offers, and what's installed.
type Service struct {
	store  *Store
	models *modelCache
	runner *sandbox.Runner
	root   string // the workspaces folder in use
}

// NewService serves the settings in store, and checks the sandbox runner.
// root is the workspaces folder in use.
func NewService(store *Store, runner *sandbox.Runner, root string) *Service {
	return &Service{store: store, models: newModelCache(), runner: runner, root: root}
}

// Load returns the saved settings. Without a saved agent it picks the first
// one that's installed, or the first one listed if none is.
func (s *Service) Load() Prefs { return s.store.Load() }

// Save stores the settings. Running agents pick up a new agent, model or
// effort with the next message once they're idle.
func (s *Service) Save(p Prefs) error { return s.store.Save(p) }

// Models lists the models an agent offers, with their reasoning efforts.
// Codex is asked live; answers are kept for a few minutes.
func (s *Service) Models(provider string) ([]agent.Model, error) {
	p, ok := agent.Lookup(provider)
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", provider)
	}
	return s.models.list(p)
}

// Environment reports what's installed.
func (s *Service) Environment() Environment {
	env := Environment{
		Root:        s.root,
		DefaultRoot: workspace.DefaultRoot(),
		RootFromEnv: os.Getenv("TUTORTOP_HOME") != "",
	}
	for _, p := range agent.Providers() {
		path, err := exec.LookPath(p.Binary)
		env.Providers = append(env.Providers, ProviderStatus{ID: string(p.ID), Name: p.Name, Installed: err == nil, Path: path})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.runner.Available(ctx); err != nil {
		env.SandboxHint = err.Error()
	} else {
		env.SandboxOK = true
	}
	return env
}

// Environment is what's installed: the agents' CLIs and a working sandbox,
// and the workspaces folder in use.
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

// ProviderStatus is whether an agent's CLI is installed, and where.
type ProviderStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
}

// modelCache keeps the models each agent offers for a few minutes, as
// asking Codex takes a while.
type modelCache struct {
	mu    sync.Mutex
	lists map[agent.ProviderID]modelList
}

type modelList struct {
	models []agent.Model
	at     time.Time
}

func newModelCache() *modelCache {
	return &modelCache{lists: map[agent.ProviderID]modelList{}}
}

// list returns the models p offers, asking it if the last answer is more
// than a few minutes old.
func (c *modelCache) list(p agent.Provider) ([]agent.Model, error) {
	c.mu.Lock()
	cached, ok := c.lists[p.ID]
	c.mu.Unlock()
	if ok && time.Since(cached.at) < 10*time.Minute {
		return cached.models, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := p.Models(ctx)
	if err != nil {
		return nil, fmt.Errorf("couldn't list %s models: %w", p.Name, err)
	}
	c.mu.Lock()
	c.lists[p.ID] = modelList{models, time.Now()}
	c.mu.Unlock()
	return models, nil
}
