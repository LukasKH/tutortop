package settings

import (
	"os"
	"path/filepath"
	"testing"
)

// Settings saved before the model choices were renamed keep them.
func TestSettingsLoadTheOldTutorKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	old := "agent: codex\ntutor:\n  codex:\n    model: gpt-5\n    effort: high\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	want := ModelChoice{Model: "gpt-5", Effort: "high"}
	if got := s.Load().Models["codex"]; got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if err := s.Save(s.Load()); err != nil {
		t.Fatal(err)
	}
	if got := s.Load().Models["codex"]; got != want {
		t.Fatalf("after saving: got %+v, want %+v", got, want)
	}
}
