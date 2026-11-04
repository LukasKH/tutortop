package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/LukasKH/tutortop/internal/workspace"
	"gopkg.in/yaml.v3"
)

// Settings are app-wide preferences, kept in settings.yaml in the user's
// config folder. They apply to every workspace.
type Settings struct {
	// Provider is the coding agent every thread runs on, tutors and writers
	// alike, an agent.Provider ID. It's "agent" in settings.yaml and to the
	// frontend, as the Settings screen calls it.
	Provider string `yaml:"agent" json:"agent"`
	// Models holds the model and reasoning effort chosen for each agent, so
	// switching agents and back keeps both choices. Files saved before it
	// was renamed have it under "tutor" (loadSettings).
	Models map[string]ModelChoice `yaml:"models" json:"models"`
	// WorkspacesFolder holds the workspaces; "" means workspace.DefaultRoot.
	// The app reads it at startup.
	WorkspacesFolder string `yaml:"workspaces_folder,omitempty" json:"workspacesFolder,omitempty"`
}

// ModelChoice is an agent's model and reasoning effort. Empty fields use the
// agent's own defaults.
type ModelChoice struct {
	Model  string `yaml:"model,omitempty" json:"model"`
	Effort string `yaml:"effort,omitempty" json:"effort"`
}

// Settings returns the saved settings. Without a saved agent it picks the
// first one that's installed, or the first one listed if none is.
func (a *App) Settings() Settings { return a.settings.Load() }

// SaveSettings stores the settings. Running agents pick up a new agent,
// model or effort with the next message once they're idle.
func (a *App) SaveSettings(s Settings) error { return a.settings.Save(s) }

// Models lists the models an agent offers, with their reasoning efforts.
// Codex is asked live; answers are kept for a few minutes.
func (a *App) Models(provider string) ([]agent.Model, error) {
	p, ok := agent.Lookup(provider)
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", provider)
	}
	return a.models.list(p)
}

// settingsStore keeps the Settings in a settings.yaml file.
type settingsStore struct {
	path string
	mu   sync.Mutex // a load doesn't read a half-done save
}

func newSettingsStore(path string) *settingsStore {
	return &settingsStore{path: path}
}

// Load returns the saved settings, with an agent picked if none is saved
// (loadSettings).
func (s *settingsStore) Load() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := os.ReadFile(s.path)
	return loadSettings(b)
}

// Save checks and stores the settings, creating a chosen workspaces folder.
func (s *settingsStore) Save(settings Settings) error {
	if !knownProvider(settings.Provider) {
		return fmt.Errorf("unknown agent %q", settings.Provider)
	}
	for id := range settings.Models {
		if !knownProvider(id) {
			delete(settings.Models, id)
		}
	}
	if settings.WorkspacesFolder != "" {
		if !filepath.IsAbs(settings.WorkspacesFolder) {
			return fmt.Errorf("the workspaces folder needs a full path, not %q", settings.WorkspacesFolder)
		}
		settings.WorkspacesFolder = filepath.Clean(settings.WorkspacesFolder)
		if err := os.MkdirAll(settings.WorkspacesFolder, 0o755); err != nil {
			return fmt.Errorf("can't use %s for workspaces: %w", settings.WorkspacesFolder, err)
		}
	}
	b, err := yaml.Marshal(settings)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return workspace.WriteFileAtomic(s.path, b)
}

// loadSettings reads settings.yaml's content, which is empty if there's no
// file. Without a saved agent it picks the first one that's installed, or
// the first one listed if none is.
func loadSettings(b []byte) Settings {
	s, _ := decode.YAML[Settings](b)
	if s.Models == nil {
		old, _ := decode.YAML[settingsBeforeModels](b)
		s.Models = old.Tutor
	}
	if s.Models == nil {
		s.Models = map[string]ModelChoice{}
	}
	if !knownProvider(s.Provider) {
		s.Provider = agent.Providers()[0].ID
		for _, p := range agent.Providers() {
			if _, err := exec.LookPath(p.Binary); err == nil {
				s.Provider = p.ID
				break
			}
		}
	}
	return s
}

// settingsBeforeModels is where settings.yaml kept the model choices before
// they were renamed Models.
type settingsBeforeModels struct {
	Tutor map[string]ModelChoice `yaml:"tutor"`
}

func knownProvider(id string) bool {
	_, ok := agent.Lookup(id)
	return ok
}

// agentConfig is what a thread's agent is started with. One started with an
// older config restarts once it's idle.
type agentConfig struct {
	Provider string
	Model    string
	Effort   string
}

func (s Settings) agentConfig() agentConfig {
	c := s.Models[s.Provider]
	return agentConfig{Provider: s.Provider, Model: c.Model, Effort: c.Effort}
}

// modelCache keeps the models each agent offers for a few minutes, as
// asking Codex takes a while.
type modelCache struct {
	mu    sync.Mutex
	lists map[string]modelList // by agent.Provider ID
}

type modelList struct {
	models []agent.Model
	at     time.Time
}

func newModelCache() *modelCache {
	return &modelCache{lists: map[string]modelList{}}
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
