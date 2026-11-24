package settings

import (
	"path/filepath"
	"testing"
)

// The rules Save holds each agent's setup to.
func TestSaveAgents(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "settings.yaml"))
	cli := filepath.Join(t.TempDir(), "bin", "..", "codex")
	for _, c := range []struct {
		name   string
		agents map[string]AgentSetup
		ok     bool
	}{
		{"another agent off", map[string]AgentSetup{"codex": {Off: true}}, true},
		{"the tutor's agent off", map[string]AgentSetup{"claude": {Off: true}}, false},
		{"a full path", map[string]AgentSetup{"codex": {Path: cli}}, true},
		{"a relative path", map[string]AgentSetup{"codex": {Path: "bin/codex"}}, false},
		{"an unknown agent", map[string]AgentSetup{"nope": {Off: true}}, true},
	} {
		err := store.Save(Prefs{Provider: "claude", Agents: c.agents})
		if (err == nil) != c.ok {
			t.Errorf("%s: Save = %v, want ok=%v", c.name, err, c.ok)
		}
	}

	if err := store.Save(Prefs{Provider: "claude", Agents: map[string]AgentSetup{"codex": {Path: cli}, "antigravity": {}, "nope": {}}}); err != nil {
		t.Fatal(err)
	}
	agents := store.Load().Agents
	if agents["codex"].Path != filepath.Clean(cli) {
		t.Errorf("path %q, want it cleaned to %q", agents["codex"].Path, filepath.Clean(cli))
	}
	if _, ok := agents["antigravity"]; ok {
		t.Error("an empty agent setup was kept")
	}
	if _, ok := agents["nope"]; ok {
		t.Error("an unknown agent was kept")
	}
}

// Hand-edited settings that leave the active agent marked off are repaired on
// load so later saves of unrelated settings still work.
func TestLoadClearsOffOnActiveProvider(t *testing.T) {
	got := load([]byte("agent: claude\nagents:\n  claude:\n    off: true\n  codex:\n    off: true\n"))
	if got.Agents["claude"].Off {
		t.Error("active provider still marked off after load")
	}
	if !got.Agents["codex"].Off {
		t.Error("other provider's off flag was cleared")
	}
	store := NewStore(filepath.Join(t.TempDir(), "settings.yaml"))
	if err := store.Save(got); err != nil {
		t.Errorf("Save after load = %v, want nil", err)
	}
}
