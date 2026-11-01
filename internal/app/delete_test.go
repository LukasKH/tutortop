package app

import (
	"context"
	"os"
	"testing"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/workspace"
)

func TestDeleteWorkspace(t *testing.T) {
	a := newTestApp(t, nil)
	var prompts []string
	a.startAgent = func(_ context.Context, _ string, _ agent.Options, _ func(agent.Event)) (agent.Session, error) {
		return &fakeSession{prompts: &prompts}, nil
	}
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	keep, err := a.store.Create("Rust", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(w.PrivateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.SendMessage(w.ID, "hello", workspace.PlanThread); err != nil {
		t.Fatal(err)
	}

	if err := a.DeleteWorkspace(w.ID); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{w.Dir, w.PrivateDir} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s still exists", dir)
		}
	}
	a.mu.Lock()
	for key := range a.sessions {
		if key.ws == w.ID {
			t.Error("a coach session survived")
		}
	}
	a.mu.Unlock()

	// A late chat write from a coach winding down doesn't bring it back.
	a.post(w, workspace.ChatItem{Thread: workspace.PlanThread, Role: workspace.RoleNotice, Text: "late"})
	if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
		t.Error("a late chat write recreated the folder")
	}
	list, _ := a.ListWorkspaces()
	if len(list) != 1 || list[0].ID != keep.ID {
		t.Fatalf("workspaces = %+v", list)
	}
	if err := a.DeleteWorkspace(w.ID); err == nil {
		t.Error("deleting twice should fail")
	}
}
