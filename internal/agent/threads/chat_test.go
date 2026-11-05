package threads

import (
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/mcp/mcptest"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Each chat thread gets its own tutor session and MCP endpoint, and answers
// to a form go back to the thread that asked.
func TestThreadsGetOwnSessions(t *testing.T) {
	fake := &heldAgents{turns: make(chan heldTurn, 8)}
	ag, store := newTestAgents(t, fake)
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
	turns := map[string]heldTurn{} // by MCP URL
	for range 2 {
		turn := fake.next(t)
		turns[turn.s.opts.MCPURL] = turn
	}
	planTurn, taskTurn := turns[ag.mcp.URL(w, workspace.PlanThread, ag)], turns[ag.mcp.URL(w, task, ag)]
	if planTurn.s == nil || taskTurn.s == nil || planTurn.s == taskTurn.s {
		t.Fatalf("sessions by MCP URL = %+v", turns)
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
	planTurn.end(nil)
	taskTurn.end(nil)
	if answers := fake.next(t); answers.s != taskTurn.s || !strings.Contains(answers.text, "this one") {
		t.Fatalf("answers went to the wrong session: %q", answers.text)
	}
	plan, _ := chat.Chat(w.ID, workspace.PlanThread.String())
	taskChat, _ := chat.Chat(w.ID, task.String())
	if len(plan) != 1 || len(taskChat) != 2 {
		t.Fatalf("plan chat %d items, task chat %d items", len(plan), len(taskChat))
	}
}
