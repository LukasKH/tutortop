package app

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Approving a plan starts one writer per upcoming objective, at the same
// time and apart from the chats; each starts fresh and is gone after its turn.
// Opening an objective that isn't written starts its own writer.
func TestWritersPrepareObjectivesInParallel(t *testing.T) {
	var mu sync.Mutex
	var started []agent.Options
	var prompts []string
	busy := map[string]bool{}
	a := newTestApp(t, func(name string, data any) {
		if ev, ok := data.(Event); ok && ev.Kind == "busy" {
			b := ev.Data.(busyEvent)
			mu.Lock()
			busy[b.Thread] = b.Busy
			mu.Unlock()
		}
	})
	a.startAgent = func(_ context.Context, _ string, opts agent.Options, _ func(agent.Event)) (agent.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		started = append(started, opts)
		return &fakeSession{prompts: &prompts, mu: &mu}, nil
	}
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	// An old conversation id must not be resumed by a writer.
	if err := w.SetSession("claude", workspace.WriterThread("go.a"), "old"); err != nil {
		t.Fatal(err)
	}
	objectives := []workspace.Objective{{ID: "go.a", Name: "A", Title: "Do A"}, {ID: "go.b", Name: "B", Title: "Do B"}, {ID: "go.c", Name: "C", Title: "Do C"}}
	if err := w.Propose(workspace.Curriculum{Language: "go", Modules: []workspace.Module{{ID: "m", Title: "M", References: []workspace.Source{{Title: "Go docs", URL: "https://go.dev/doc/"}}, Objectives: objectives}}}); err != nil {
		t.Fatal(err)
	}
	if err := a.ApproveProposal(w.ID); err != nil {
		t.Fatal(err)
	}

	settled := func(n int) []agent.Options {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			done := len(started) == n && len(prompts) == n
			a.mu.Lock()
			live := len(a.sessions)
			a.mu.Unlock()
			idle := !slices.Contains(mapValues(busy), true)
			got := slices.Clone(started)
			mu.Unlock()
			if done && live == 0 && idle {
				return got
			}
			if time.Now().After(deadline) {
				t.Fatalf("started %d writers, %d prompts, %d sessions still live, busy %v", len(got), len(prompts), live, busy)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	opts := settled(2)
	var urls []string
	for _, o := range opts {
		if !strings.Contains(o.Instructions, "writer for one objective") || o.ResumeID != "" {
			t.Fatalf("writer started with resume %q and instructions:\n%s", o.ResumeID, o.Instructions[len(o.Instructions)-300:])
		}
		urls = append(urls, o.MCPURL)
	}
	if !slices.Contains(urls, a.mcp.URL(w, workspace.WriterThread("go.a"))) || !slices.Contains(urls, a.mcp.URL(w, workspace.WriterThread("go.b"))) {
		t.Fatal("approving didn't start writers for the first two objectives")
	}
	if chat, _ := a.Chat(w.ID, workspace.PlanThread); len(chat) != 1 || chat[0].Role != workspace.RoleNotice {
		t.Fatalf("the planning chat should only note the approval: %+v", chat)
	}

	// Jumping ahead to the third objective writes it now.
	if err := a.PrepareObjective(w.ID, "go.c"); err != nil {
		t.Fatal(err)
	}
	opts = settled(3)
	if opts[2].MCPURL != a.mcp.URL(w, workspace.WriterThread("go.c")) || !strings.Contains(prompts[2], "Do C") {
		t.Fatalf("PrepareObjective started %s with %q", opts[2].MCPURL, prompts[2])
	}
}

func mapValues[K comparable, V any](m map[K]V) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// blockingSession holds every turn until release is closed.
type blockingSession struct{ release chan struct{} }

func (b *blockingSession) Prompt(ctx context.Context, _ string) error {
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return nil
}
func (b *blockingSession) ID() string   { return "" }
func (b *blockingSession) Cancel()      {}
func (b *blockingSession) Close() error { return nil }

// The workspace state says which objectives are being written and how their
// sandbox check is going, so a page opened later (or after a reload) shows
// the same thing as one that watched the events.
func TestWorkspaceStateShowsWriters(t *testing.T) {
	a := newTestApp(t, nil)
	release := make(chan struct{})
	a.startAgent = func(context.Context, string, agent.Options, func(agent.Event)) (agent.Session, error) {
		return &blockingSession{release: release}, nil
	}
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Propose(workspace.Curriculum{Language: "go", Modules: []workspace.Module{{ID: "m", Title: "M", References: []workspace.Source{{Title: "Go docs", URL: "https://go.dev/doc/"}}, Objectives: []workspace.Objective{{ID: "go.a", Name: "A", Title: "Do A"}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
	thread := workspace.WriterThread("go.a")
	if err := a.PrepareObjective(w.ID, "go.a"); err != nil {
		t.Fatal(err)
	}
	// Asking again while it writes doesn't start a second writer.
	if err := a.PrepareObjective(w.ID, "go.a"); err != nil {
		t.Fatal(err)
	}
	a.writerCheck(w.ID, GateEvent{Objective: "go.a", Title: "Add", Running: true})
	st, _ := a.Workspace(w.ID)
	if !st.Busy[thread] || !st.Checks["go.a"].Running {
		t.Fatalf("while writing: busy %v, checks %v", st.Busy, st.Checks)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		st, _ = a.Workspace(w.ID)
		if !st.Busy[thread] && len(st.Checks) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after writing: busy %v, checks %v", st.Busy, st.Checks)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
