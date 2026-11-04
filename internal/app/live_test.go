//go:build live

package app

import (
	"os"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/text"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// TestLiveInterview creates a workspace with the real Claude Code and checks
// the tutor opens with an ask_user form. Uses a little quota.
func TestLiveInterview(t *testing.T) {
	provider := os.Getenv("TUTORTOP_PROVIDER")
	if provider == "" {
		provider = "claude"
	}

	a := newTestApp(t)
	defer a.ServiceShutdown()

	if err := a.SaveSettings(Settings{Agent: provider}); err != nil {
		t.Fatal(err)
	}
	id, err := a.CreateWorkspace("Go", "")
	if err != nil {
		t.Fatal(err)
	}
	// The tutor's questions show up in the workspace once it calls ask_user.
	var questions *workspace.Questions
	for deadline := time.Now().Add(4 * time.Minute); questions == nil && time.Now().Before(deadline); time.Sleep(2 * time.Second) {
		st, err := a.Workspace(id)
		if err != nil {
			t.Fatal(err)
		}
		questions = st.Questions
	}
	chat, _ := a.Chat(id, workspace.PlanThread.String())
	for _, c := range chat {
		t.Logf("%-9s %-7s %s", c.Role, c.Status, text.Truncate(c.Text, 300))
	}
	if questions == nil {
		t.Fatal("tutor didn't call ask_user")
	}
	t.Logf("questions: %+v", *questions)
}
