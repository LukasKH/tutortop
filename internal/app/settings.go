package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/workspace"
	"gopkg.in/yaml.v3"
)

// Settings are app-wide preferences, kept in settings.yaml in the user's
// config folder. They apply to every workspace.
type Settings struct {
	// Agent is the coding agent that tutors, an agent.Provider ID.
	Agent string `yaml:"agent" json:"agent"`
	// Tutor holds the model and reasoning effort chosen for each agent, so
	// switching agents and back keeps both choices.
	Tutor map[string]TutorChoice `yaml:"tutor" json:"tutor"`
	// WorkspacesFolder holds the workspaces; "" means workspace.DefaultRoot.
	// The app reads it at startup.
	WorkspacesFolder string `yaml:"workspaces_folder,omitempty" json:"workspacesFolder,omitempty"`
}

// TutorChoice is an agent's model and reasoning effort. Empty fields use the
// agent's own defaults.
type TutorChoice struct {
	Model  string `yaml:"model,omitempty" json:"model"`
	Effort string `yaml:"effort,omitempty" json:"effort"`
}

// tutorConfig is what a tutor session is started with. A session started
// with an older config restarts once it's idle.
type tutorConfig struct {
	Agent  string
	Model  string
	Effort string
}

func (s Settings) tutor() tutorConfig {
	c := s.Tutor[s.Agent]
	return tutorConfig{Agent: s.Agent, Model: c.Model, Effort: c.Effort}
}

// Settings returns the saved settings. Without a saved agent it picks the
// first one that's installed, or the first one listed if none is.
func (a *App) Settings() Settings {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	return a.loadSettings()
}

func (a *App) loadSettings() Settings {
	var s Settings
	if b, err := os.ReadFile(a.settingsPath); err == nil {
		_ = yaml.Unmarshal(b, &s)
	}
	if s.Tutor == nil {
		s.Tutor = map[string]TutorChoice{}
	}
	if !knownProvider(s.Agent) {
		s.Agent = agent.Providers()[0].ID
		for _, p := range agent.Providers() {
			if _, err := exec.LookPath(p.Binary); err == nil {
				s.Agent = p.ID
				break
			}
		}
	}
	return s
}

// SaveSettings stores the settings. Tutor sessions pick up a new agent,
// model or effort with the next message once they're idle.
func (a *App) SaveSettings(s Settings) error {
	if !knownProvider(s.Agent) {
		return fmt.Errorf("unknown agent %q", s.Agent)
	}
	for id := range s.Tutor {
		if !knownProvider(id) {
			delete(s.Tutor, id)
		}
	}
	if s.WorkspacesFolder != "" {
		if !filepath.IsAbs(s.WorkspacesFolder) {
			return fmt.Errorf("the workspaces folder needs a full path, not %q", s.WorkspacesFolder)
		}
		s.WorkspacesFolder = filepath.Clean(s.WorkspacesFolder)
		if err := os.MkdirAll(s.WorkspacesFolder, 0o755); err != nil {
			return fmt.Errorf("can't use %s for workspaces: %w", s.WorkspacesFolder, err)
		}
	}
	b, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.settingsPath), 0o755); err != nil {
		return err
	}
	return workspace.WriteFileAtomic(a.settingsPath, b)
}

func knownProvider(id string) bool {
	_, ok := agent.Lookup(id)
	return ok
}

// Models lists the models an agent offers, with their reasoning efforts.
// Codex is asked live; answers are kept for a few minutes.
func (a *App) Models(provider string) ([]agent.Model, error) {
	p, ok := agent.Lookup(provider)
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", provider)
	}
	a.settingsMu.Lock()
	cached, ok := a.models[provider]
	a.settingsMu.Unlock()
	if ok && time.Since(cached.at) < 10*time.Minute {
		return cached.models, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := p.Models(ctx)
	if err != nil {
		return nil, fmt.Errorf("couldn't list %s models: %w", p.Name, err)
	}
	a.settingsMu.Lock()
	a.models[provider] = modelList{models, time.Now()}
	a.settingsMu.Unlock()
	return models, nil
}

type modelList struct {
	models []agent.Model
	at     time.Time
}
