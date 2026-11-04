package threads

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/agent/threads/threadstest"
	"github.com/LukasKH/tutortop/internal/mcp/mcptest"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Each chat thread gets its own tutor session and MCP endpoint, and answers
// to a form go back to the thread that asked.
func TestThreadsGetOwnSessions(t *testing.T) {
	var mu sync.Mutex
	sessions := map[string]*threadstest.Session{} // by MCP URL
	ag, store := newTestAgents(t, threadstest.CLI(func(_ context.Context, _ string, opts agent.Options, _ func(agent.Event)) (agent.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		s := &threadstest.Session{}
		sessions[opts.MCPURL] = s
		return s, nil
	}))
	prompts := func(w *workspace.Workspace, thread workspace.Thread) []string {
		mu.Lock()
		defer mu.Unlock()
		if s := sessions[ag.mcp.URL(w, thread, ag)]; s != nil {
			return s.Prompts()
		}
		return nil
	}
	chat := NewChatService(store, ag)
	w, err := store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	task := workspace.TaskThread("0001-some-task")
	if err := chat.SendMessage(w.ID, "hello plan", workspace.PlanThread.String()); err != nil {
		t.Fatal(err)
	}
	if err := chat.SendMessage(w.ID, "hello task", task.String()); err != nil {
		t.Fatal(err)
	}
	ask := mcptest.Caller(t, ag.mcp.URL(w, task, ag))
	if out, isErr := ask("ask_user", map[string]any{"questions": []any{
		map[string]any{"id": "q1", "question": "Which?"},
		map[string]any{"id": "q2", "question": "Why?"},
	}}); isErr {
		t.Fatalf("ask_user: %s", out)
	}
	if q := w.PendingQuestions(); q == nil || q.Thread != task.String() || len(q.Questions) != 2 {
		t.Fatalf("pending = %+v", q)
	}
	if err := chat.AnswerQuestions(w.ID, map[string]string{"q1": "this one", "q2": "because"}); err != nil {
		t.Fatal(err)
	}

	eventually(t, "both threads prompted", func() bool {
		return len(prompts(w, workspace.PlanThread)) == 1 && len(prompts(w, task)) == 2
	})
	if p := prompts(w, task); !strings.Contains(p[1], "this one") {
		t.Fatalf("answers went elsewhere: %q", p)
	}
	plan, _ := chat.Chat(w.ID, workspace.PlanThread.String())
	taskChat, _ := chat.Chat(w.ID, task.String())
	if len(plan) != 1 || len(taskChat) != 2 {
		t.Fatalf("plan chat %d items, task chat %d items", len(plan), len(taskChat))
	}
}
