package app

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/workspace"
)

func TestSettingsRoundTrip(t *testing.T) {
	a := newTestApp(t, nil)
	want := Settings{Agent: "codex", Coach: map[string]CoachChoice{"codex": {Model: "gpt-x", Effort: "high"}, "claude": {Model: "opus"}}}
	if err := a.SaveSettings(want); err != nil {
		t.Fatal(err)
	}
	got := a.Settings()
	if got.Agent != "codex" || got.Coach["codex"] != want.Coach["codex"] || got.Coach["claude"].Model != "opus" {
		t.Fatalf("got %+v", got)
	}
	if err := a.SaveSettings(Settings{Agent: "nope"}); err == nil {
		t.Fatal("an unknown agent was accepted")
	}
}

// A changed setting reaches the coach on the next message; switching agents
// starts fresh sessions instead of resuming the other agent's.
func TestSettingsChangeRestartsIdleCoach(t *testing.T) {
	a := newTestApp(t, nil)
	type start struct {
		provider string
		opts     agent.Options
	}
	var mu sync.Mutex
	var starts []start
	var prompts []string
	a.startAgent = func(_ context.Context, provider string, opts agent.Options, _ func(agent.Event)) (agent.Session, error) {
		mu.Lock()
		starts = append(starts, start{provider, opts})
		n := len(starts)
		mu.Unlock()
		return &fakeSession{id: provider + "-session-" + strconv.Itoa(n), prompts: &prompts, mu: &mu}, nil
	}
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	send := func(text string, wantStarts int) start {
		t.Helper()
		if err := a.SendMessage(w.ID, text, workspace.PlanThread); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			n, p := len(starts), len(prompts)
			mu.Unlock()
			if n == wantStarts && p > 0 && !coachBusy(a, w.ID) {
				mu.Lock()
				defer mu.Unlock()
				return starts[n-1]
			}
			if time.Now().After(deadline) {
				t.Fatalf("after %q: %d starts, want %d", text, n, wantStarts)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	if err := a.SaveSettings(Settings{Agent: "claude", Coach: map[string]CoachChoice{"claude": {Model: "sonnet", Effort: "low"}}}); err != nil {
		t.Fatal(err)
	}
	s := send("one", 1)
	if s.provider != "claude" || s.opts.Model != "sonnet" || s.opts.Effort != "low" {
		t.Fatalf("first start = %+v", s)
	}
	send("two", 1) // unchanged settings reuse the session

	if err := a.SaveSettings(Settings{Agent: "claude", Coach: map[string]CoachChoice{"claude": {Model: "opus", Effort: "max"}}}); err != nil {
		t.Fatal(err)
	}
	s = send("three", 2)
	if s.opts.Model != "opus" || s.opts.Effort != "max" || s.opts.ResumeID != "claude-session-1" {
		t.Fatalf("restart after a model change = %+v", s.opts)
	}

	if err := a.SaveSettings(Settings{Agent: "codex"}); err != nil {
		t.Fatal(err)
	}
	s = send("four", 3)
	if s.provider != "codex" || s.opts.ResumeID != "" {
		t.Fatalf("switching agents should start fresh: %+v", s)
	}
}

// coachBusy reports whether any of a workspace's coaches is working.
func coachBusy(a *App, wsID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, s := range a.sessions {
		if key.ws == wsID && (s.busy || len(s.queue) > 0) {
			return true
		}
	}
	return false
}
