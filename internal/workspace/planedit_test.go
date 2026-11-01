package workspace

import (
	"strings"
	"testing"
)

func TestSaveMission(t *testing.T) {
	s := &Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	w, err := s.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := w.SaveMission("# Mission\n\nShip a notes app.")
	if err != nil || !changed {
		t.Fatalf("first save: changed %v, %v", changed, err)
	}
	if !strings.Contains(w.ReadDoc("MISSION.md"), "Ship a notes app.") {
		t.Error("mission not saved")
	}
	// The same text again, give or take spaces, changes nothing.
	if changed, _ := w.SaveMission("\n# Mission\n\nShip a notes app.\n\n"); changed {
		t.Error("an unchanged save reported a change")
	}
}
