package threads

import (
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/mcp"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// What the agent does between its messages and tool calls shows live: its
// reasoning, then a long tool call it writes. The next message or tool call,
// or the turn's end, clears it, and none of it is saved.
func TestTranscriptShowsWorkingLive(t *testing.T) {
	w := newTestWorkspace(t)
	shown := captureWorking(t, w.ID)
	tr := newTranscript(w, "plan")
	curriculum := agent.Tool{ID: "t2", Kind: agent.ToolOther, MCPServer: mcp.Name, MCPTool: "propose_curriculum"}
	running := curriculum
	running.Status = agent.ToolRunning
	for _, ev := range []agent.Event{
		{Kind: agent.EventThinking, Text: "Reading "},
		{Kind: agent.EventThinking, Text: "your notes"},
		// Quick to write: nothing to show.
		{Kind: agent.EventPreparing, Tool: agent.Tool{ID: "t1", Kind: agent.ToolOther, MCPServer: mcp.Name, MCPTool: "get_state"}},
		{Kind: agent.EventPreparing, Tool: curriculum},
		{Kind: agent.EventTool, Tool: running},
		{Kind: agent.EventTool, Tool: agent.Tool{ID: "t2", Status: agent.ToolDone}},
		{Kind: agent.EventThinking, Text: "Done"},
		{Kind: agent.EventText, Text: "Here "},
		{Kind: agent.EventText, Text: "it is."},
		{Kind: agent.EventPreparing, Tool: agent.Tool{ID: "t3", Kind: agent.ToolEdit}},
	} {
		tr.handle(ev)
	}
	tr.endTurn()

	want := []events.Working{
		{Thinking: "Reading "},
		{Thinking: "Reading your notes"},
		{Thinking: "Reading your notes", Preparing: "Drafting your plan"},
		{},
		{Thinking: "Done"},
		{},
		{Preparing: "Writing a file"},
		{},
	}
	for i := range want {
		want[i].Workspace, want[i].Thread = w.ID, "plan"
	}
	if got := shown(); !reflect.DeepEqual(got, want) {
		t.Errorf("shown:\n%+v\nwant:\n%+v", got, want)
	}
	chat, _ := w.Chat("plan")
	if len(chat) != 1 || chat[0].Role != workspace.RoleAssistant || chat[0].Text != "Here it is." {
		t.Errorf("chat = %+v, want only the reply", chat)
	}
}

// Long reasoning reaches the UI by its latest part, so updates stay small.
func TestTranscriptKeepsLatestThinking(t *testing.T) {
	w := newTestWorkspace(t)
	shown := captureWorking(t, w.ID)
	tr := newTranscript(w, "plan")
	tr.handle(agent.Event{Kind: agent.EventThinking, Text: strings.Repeat("a", maxThinking)})
	tr.handle(agent.Event{Kind: agent.EventThinking, Text: "latest"})
	got := shown()
	last := got[len(got)-1].Thinking
	if !strings.HasSuffix(last, "latest") || len(last) > maxThinking+len("…") {
		t.Errorf("thinking %d bytes ending %q, want at most %d ending in the latest", len(last), last[max(0, len(last)-10):], maxThinking)
	}
}

// captureWorking collects the workspace's thread:working events until the
// test ends.
func captureWorking(t *testing.T, wsID string) func() []events.Working {
	t.Helper()
	var mu sync.Mutex
	var got []events.Working
	events.SetEmitter(func(name string, data any) {
		if w, ok := data.(events.Working); ok && name == string(events.ThreadWorking) && w.Workspace == wsID {
			mu.Lock()
			got = append(got, w)
			mu.Unlock()
		}
	})
	t.Cleanup(func() { events.SetEmitter(nil) })
	return func() []events.Working {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}
