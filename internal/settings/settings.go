// Package settings keeps the app-wide settings in settings.yaml, and serves
// the Settings screen (Service).
package settings

import (
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
	// switching agents and back keeps both choices. Files saved before it
	// was renamed have it under "tutor" (load).
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
		old, _ := decode.YAML[prefsBeforeModels](b)
		s.Models = old.Tutor
	}
	if s.Models == nil {
		s.Models = map[string]ModelChoice{}
	}
	if !knownProvider(s.Provider) {
		s.Provider = string(agent.Providers()[0].ID)
		for _, p := range agent.Providers() {
			if _, err := exec.LookPath(p.Binary); err == nil {
				s.Provider = string(p.ID)
				break
			}
		}
	}
	return s
}

// prefsBeforeModels is where settings.yaml kept the model choices before
// they were renamed Models.
type prefsBeforeModels struct {
	Tutor map[string]ModelChoice `yaml:"tutor"`
}

func knownProvider(id string) bool {
	_, ok := agent.Lookup(id)
	return ok
}
