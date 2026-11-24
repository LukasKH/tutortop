// Package settings keeps the app-wide settings in settings.yaml, and serves
// the Settings screen (Service).
package settings

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/atomicfile"
	"github.com/LukasKH/tutortop/internal/decode"
	"gopkg.in/yaml.v3"
)

// Prefs are the app-wide settings, kept in settings.yaml in the user's
// config folder. They apply to every workspace.
type Prefs struct {
	// Provider is the coding agent every thread runs on, tutors and writers
	// alike, an agent.Provider ID. It's "agent" in settings.yaml and to the
	// frontend, as the Settings screen calls it.
	Provider string `yaml:"agent" json:"agent"`
	// Models holds the model and reasoning effort chosen for each agent, so
	// switching agents and back keeps both choices.
	Models map[string]ModelChoice `yaml:"models" json:"models"`
	// Agents holds each agent's setup: whether it's offered, and which CLI
	// to run. An agent missing here is on and found on PATH.
	Agents map[string]AgentSetup `yaml:"agents,omitempty" json:"agents"`
	// WorkspacesFolder holds the workspaces; "" means workspace.DefaultRoot.
	// The app reads it at startup.
	WorkspacesFolder string `yaml:"workspaces_folder,omitempty" json:"workspacesFolder,omitempty"`
	// Copilot shows GitHub Copilot's suggestions in the task editor, one line
	// at a time (internal/copilot). Off by default: it sends the file being
	// edited to GitHub.
	Copilot bool `yaml:"copilot,omitempty" json:"copilot"`
}

// binary is the CLI to run for an agent: the path set up for it, or its name.
func (p Prefs) binary(provider agent.Provider) string {
	return cmp.Or(p.Agents[string(provider.ID)].Path, provider.Binary)
}

// ModelChoice is an agent's model and reasoning effort. Empty fields use the
// agent's own defaults.
type ModelChoice struct {
	Model  string `yaml:"model,omitempty" json:"model"`
	Effort string `yaml:"effort,omitempty" json:"effort"`
}

// AgentSetup is how one agent is set up.
type AgentSetup struct {
	// Off leaves the agent out of the tutor picker. The agent in use can't
	// be off.
	Off bool `yaml:"off,omitempty" json:"off"`
	// Path is the agent's CLI as a full path; "" finds agent.Provider.Binary
	// on PATH.
	Path string `yaml:"path,omitempty" json:"path"`
}

// Store keeps the Prefs in a settings.yaml file.
type Store struct {
	path string
	mu   sync.Mutex // a load doesn't read a half-done save
}

// NewStore keeps the settings in the file at path, which needn't exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Load returns the saved settings, with an agent picked if none is saved
// (load).
func (s *Store) Load() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := os.ReadFile(s.path)
	return load(b)
}

// Save checks and stores the settings, creating a chosen workspaces folder.
func (s *Store) Save(p Prefs) error {
	if !knownProvider(p.Provider) {
		return fmt.Errorf("unknown agent %q", p.Provider)
	}
	for id := range p.Models {
		if !knownProvider(id) {
			delete(p.Models, id)
		}
	}
	for id, a := range p.Agents {
		switch {
		case !knownProvider(id) || a == (AgentSetup{}):
			delete(p.Agents, id)
		case a.Off && id == p.Provider:
			return fmt.Errorf("the tutor's agent can't be turned off; choose another agent first")
		case a.Path != "" && !filepath.IsAbs(a.Path):
			return fmt.Errorf("the agent's CLI needs a full path, not %q", a.Path)
		case a.Path != "":
			a.Path = filepath.Clean(a.Path)
			p.Agents[id] = a
		}
	}
	if p.WorkspacesFolder != "" {
		if !filepath.IsAbs(p.WorkspacesFolder) {
			return fmt.Errorf("the workspaces folder needs a full path, not %q", p.WorkspacesFolder)
		}
		p.WorkspacesFolder = filepath.Clean(p.WorkspacesFolder)
		if err := os.MkdirAll(p.WorkspacesFolder, 0o755); err != nil {
			return fmt.Errorf("can't use %s for workspaces: %w", p.WorkspacesFolder, err)
		}
	}
	b, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(s.path, b)
}

// load reads settings.yaml's content, which is empty if there's no file.
// Without a saved agent it picks the first one that's installed, or the
// first one listed if none is.
func load(b []byte) Prefs {
	s, _ := decode.YAML[Prefs](b)
	if s.Models == nil {
		s.Models = map[string]ModelChoice{}
	}
	if s.Agents == nil {
		s.Agents = map[string]AgentSetup{}
	}
	if !knownProvider(s.Provider) {
		s.Provider = installedProvider(s)
	}
	// The tutor's agent can't be off; clearing a stale flag from a hand-edited
	// file keeps later saves of unrelated settings from failing.
	if a := s.Agents[s.Provider]; a.Off {
		a.Off = false
		if a == (AgentSetup{}) {
			delete(s.Agents, s.Provider)
		} else {
			s.Agents[s.Provider] = a
		}
	}
	return s
}

// installedProvider is the first enabled agent whose CLI is installed, or
// the first one listed if none is.
func installedProvider(s Prefs) string {
	for _, p := range agent.Providers() {
		if s.Agents[string(p.ID)].Off {
			continue
		}
		if _, err := exec.LookPath(s.binary(p)); err == nil {
			return string(p.ID)
		}
	}
	return string(agent.Providers()[0].ID)
}

func knownProvider(id string) bool {
	_, ok := agent.Lookup(id)
	return ok
}
