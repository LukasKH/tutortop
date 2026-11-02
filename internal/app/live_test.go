//go:build live

package app

import (
	"os"
	"sync"
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

	var mu sync.Mutex
	var kinds []string
	var questions any
	idle := make(chan struct{}, 8)
	a := newTestApp(t, func(_ string, data any) {
		ev := data.(Event)
		mu.Lock()
		defer mu.Unlock()
		kinds = append(kinds, ev.Kind)
		if ev.Kind == "questions" {
			questions = ev.Data
		}
		if b, ok := ev.Data.(busyEvent); ev.Kind == "busy" && ok && !b.Busy {
			idle <- struct{}{}
		}
	})
	defer a.ServiceShutdown()

	if err := a.SaveSettings(Settings{Agent: provider}); err != nil {
		t.Fatal(err)
	}
	id, err := a.CreateWorkspace("Go", "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-idle:
	case <-time.After(4 * time.Minute):
		t.Fatal("tutor didn't finish its first turn")
	}
	chat, _ := a.Chat(id, workspace.PlanThread)
	for _, c := range chat {
		t.Logf("%-9s %-7s %s", c.Role, c.Status, text.Truncate(c.Text, 300))
	}
	mu.Lock()
	defer mu.Unlock()
	if questions == nil {
		t.Fatalf("tutor didn't call ask_user; events: %v", kinds)
	}
	t.Logf("questions: %+v", questions)
}
