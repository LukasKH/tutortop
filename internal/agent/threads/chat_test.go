package threads

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/agent/threads/threadstest"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Each chat thread gets its own tutor session, MCP endpoint and role, and
// answers to a form go back to the thread that asked.
func TestThreadsGetOwnSessions(t *testing.T) {
	type started struct {
		opts    agent.Options
		prompts []string
	}
	var mu sync.Mutex
	sessions := map[string]*started{} // by MCP URL
	ag, store := newTestAgents(t, threadstest.CLI(func(_ context.Context, _ string, opts agent.Options, _ func(agent.Event)) (agent.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		s := &started{opts: opts}
		sessions[opts.MCPURL] = s
		return &threadstest.Session{Prompts: &s.prompts, Mu: &mu}, nil
	}))
	chat := NewChatService(store, ag)
	w, err := store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := chat.SendMessage(w.ID, "hello plan", workspace.PlanThread.String()); err != nil {
		t.Fatal(err)
	}
	if err := chat.SendMessage(w.ID, "hello task", "0001-some-task"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetQuestions(&workspace.Questions{Thread: "0001-some-task", Questions: []workspace.Question{{ID: "q", Question: "Which?"}}}); err != nil {
		t.Fatal(err)
	}
	if err := chat.AnswerQuestions(w.ID, map[string]string{"q": "this one"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		plan, task := sessions[ag.mcp.URL(w, workspace.PlanThread, ag)], sessions[ag.mcp.URL(w, workspace.TaskThread("0001-some-task"), ag)]
		done := plan != nil && task != nil && len(plan.prompts) == 1 && len(task.prompts) == 2
		mu.Unlock()
		if done {
			if !strings.Contains(plan.opts.Instructions, "planner") || !strings.Contains(task.opts.Instructions, "tutor for one task") {
				t.Fatal("threads didn't get their role instructions")
			}
			if !strings.Contains(task.prompts[1], "this one") {
				t.Fatalf("answers went elsewhere: %q", task.prompts)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sessions: %d", len(sessions))
		}
		time.Sleep(10 * time.Millisecond)
	}
	plan, _ := chat.Chat(w.ID, workspace.PlanThread.String())
	task, _ := chat.Chat(w.ID, "0001-some-task")
	if len(plan) != 1 || len(task) != 2 {
		t.Fatalf("plan chat %d items, task chat %d items", len(plan), len(task))
	}
}
