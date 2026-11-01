package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
)

// Settings are app-wide preferences, kept in the user's config folder. They
// apply to every workspace.
type Settings struct {
	// Agent is the coding agent that coaches: "claude" or "codex".
	Agent string `json:"agent"`
	// Coach holds the model and reasoning effort chosen for each agent, so
	// switching agents and back keeps both choices.
	Coach map[string]CoachChoice `json:"coach"`
}

// CoachChoice is an agent's model and reasoning effort. Empty fields use the
// agent's own defaults.
type CoachChoice struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// coachConfig is what a coach session is started with. A session started
// with an older config restarts once it's idle.
type coachConfig struct {
	Agent  string
	Model  string
	Effort string
}

func (s Settings) coach() coachConfig {
	c := s.Coach[s.Agent]
	return coachConfig{Agent: s.Agent, Model: c.Model, Effort: c.Effort}
}

// Settings returns the saved settings. Without a saved agent it picks the
// first one that's installed.
func (a *App) Settings() Settings {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	return a.loadSettings()
}

func (a *App) loadSettings() Settings {
	var s Settings
	if b, err := os.ReadFile(a.settingsPath); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Coach == nil {
		s.Coach = map[string]CoachChoice{}
	}
	if !knownProvider(s.Agent) {
		s.Agent = "claude"
		for _, p := range agent.Providers() {
			if _, err := exec.LookPath(p.Binary); err == nil {
				s.Agent = p.ID
				break
			}
		}
	}
	return s
}

// SaveSettings stores the settings. Coach sessions pick up a new agent,
// model or effort with the next message once they're idle.
func (a *App) SaveSettings(s Settings) error {
	if !knownProvider(s.Agent) {
		return fmt.Errorf("unknown agent %q", s.Agent)
	}
	for id := range s.Coach {
		if !knownProvider(id) {
			delete(s.Coach, id)
		}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.settingsPath), 0o755); err != nil {
		return err
	}
	tmp := a.settingsPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.settingsPath)
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
