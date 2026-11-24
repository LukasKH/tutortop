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
	"golang.org/x/sync/singleflight"
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
// Codex and Antigravity are asked live; answers are kept for a few minutes.
func (s *Service) Models(ctx context.Context, provider string) ([]agent.Model, error) {
	p, ok := agent.Lookup(provider)
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", provider)
	}
	return s.models.list(ctx, p, s.store.Load().binary(p))
}

// Check tries an agent's CLI: its version, its sign-in and its models. The
// models it finds replace the cached ones.
func (s *Service) Check(ctx context.Context, provider string) (agent.Check, error) {
	p, ok := agent.Lookup(provider)
	if !ok {
		return agent.Check{}, fmt.Errorf("unknown agent %q", provider)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	binary := s.store.Load().binary(p)
	c := p.Check(ctx, binary)
	if c.Problem == "" {
		s.models.put(p, binary, c.Models)
	} else {
		s.models.drop(p, binary)
	}
	return c, nil
}

// Environment reports what's installed.
func (s *Service) Environment() Environment {
	env := Environment{
		Providers:   []ProviderStatus{},
		Root:        s.root,
		DefaultRoot: workspace.DefaultRoot(),
		RootFromEnv: os.Getenv("TUTORTOP_HOME") != "",
	}
	prefs := s.store.Load()
	for _, p := range agent.Providers() {
		path, err := exec.LookPath(prefs.binary(p))
		env.Providers = append(env.Providers, ProviderStatus{ID: string(p.ID), Name: p.Name, Binary: p.Binary, Installed: err == nil, Path: path})
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
	Binary    string `json:"binary"` // the CLI's name, looked up on PATH unless the settings give a path
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
}

// modelCache keeps the models each agent's CLI offers for a few minutes, as
// asking Codex or Antigravity takes a while.
type modelCache struct {
	mu    sync.Mutex
	lists map[modelKey]modelList
	asks  singleflight.Group // one CLI probe per modelKey at a time
}

// modelKey is an agent and the CLI asked, so changing the CLI's path asks again.
type modelKey struct {
	id     agent.ProviderID
	binary string
}

type modelList struct {
	models []agent.Model
	at     time.Time
}

func newModelCache() *modelCache {
	return &modelCache{lists: map[modelKey]modelList{}}
}

// list returns the models p's CLI binary offers, asking it if the last
// answer is more than a few minutes old.
//
// Callers asking at once share one probe. It runs on its own deadline, so a
// caller that gives up doesn't cut it short for the others.
func (c *modelCache) list(ctx context.Context, p agent.Provider, binary string) ([]agent.Model, error) {
	c.mu.Lock()
	cached, ok := c.lists[modelKey{p.ID, binary}]
	c.mu.Unlock()
	if ok && time.Since(cached.at) < 10*time.Minute {
		return cached.models, nil
	}
	ask := c.asks.DoChan(string(p.ID)+"\x00"+binary, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		models, err := p.Models(ctx, binary)
		if err != nil {
			return nil, fmt.Errorf("couldn't list %s models: %w", p.Name, err)
		}
		c.put(p, binary, models)
		return models, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ask:
		models, _ := res.Val.([]agent.Model) // nil with an error
		return models, res.Err
	}
}

func (c *modelCache) put(p agent.Provider, binary string, models []agent.Model) {
	c.mu.Lock()
	c.lists[modelKey{p.ID, binary}] = modelList{models, time.Now()}
	c.mu.Unlock()
}

func (c *modelCache) drop(p agent.Provider, binary string) {
	c.mu.Lock()
	delete(c.lists, modelKey{p.ID, binary})
	c.mu.Unlock()
}
