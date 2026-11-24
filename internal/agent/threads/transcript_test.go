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
	} {
		tr.handle(ev)
	}
	// While a hidden MCP tool runs (e.g. checking links), its preparing label stays visible.
	if got := shown(); len(got) == 0 || got[len(got)-1].Preparing != "Drafting your plan" {
		t.Fatalf("working cleared while hidden tool still running: %+v", got)
	}
	for _, ev := range []agent.Event{
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

// The learner's chat keeps the tutor's finished work and real problems. Shell
// commands, which the policy always refuses, and calls that fail and leave
// the tutor to recover aren't saved.
func TestTranscriptSavesOnlyWorkThatHappened(t *testing.T) {
	w := newTestWorkspace(t)
	tr := newTranscript(w, "plan")
	tool := func(tool agent.Tool) { tr.handle(agent.Event{Kind: agent.EventTool, Tool: tool}) }

	tool(agent.Tool{ID: "1", Status: agent.ToolRunning, Kind: agent.ToolEdit, Paths: []string{"MISSION.md"}})
	tool(agent.Tool{ID: "1", Status: agent.ToolDone})
	tool(agent.Tool{ID: "2", Status: agent.ToolRunning, Kind: agent.ToolExecute, Title: "curl.exe -sI https://go.dev/blog/range-functions"})
	tool(agent.Tool{ID: "2", Status: agent.ToolFailed})
	tool(agent.Tool{ID: "3", Status: agent.ToolRunning, Kind: agent.ToolFetch, Title: "Fetched https://go.dev"})
	tool(agent.Tool{ID: "3", Status: agent.ToolFailed})
	tr.handle(agent.Event{Kind: agent.EventWarning, Text: "Tutortop's tools didn't connect"})
	tool(agent.Tool{ID: "4", Status: agent.ToolRunning, Kind: agent.ToolFetch, Title: "Searched the web: iterators"})
	tr.endTurn()

	chat, err := w.Chat("plan")
	if err != nil {
		t.Fatal(err)
	}
	type line struct {
		role   workspace.ChatRole
		text   string
		status workspace.ChatStatus
	}
	want := []line{
		{workspace.RoleTool, "Updated your mission", workspace.ChatDone},
		{workspace.RoleNotice, "Tutortop's tools didn't connect", workspace.ChatFailed},
		{workspace.RoleTool, "Searched the web: iterators", workspace.ChatDone},
	}
	if len(chat) != len(want) {
		t.Fatalf("chat = %+v, want %+v", chat, want)
	}
	for i, c := range chat {
		if got := (line{c.Role, c.Text, c.Status}); got != want[i] {
			t.Errorf("chat[%d] = %+v, want %+v", i, got, want[i])
		}
	}
}

// Closing a transcript (on shutdown or stop) saves streamed text received so
// far, clears working state, and ignores any late events from a dying CLI.
func TestTranscriptCloseSavesStreamedTextAndIgnoresLateEvents(t *testing.T) {
	w := newTestWorkspace(t)
	shown := captureWorking(t, w.ID)
	tr := newTranscript(w, "plan")
	tr.handle(agent.Event{Kind: agent.EventThinking, Text: "Thinking"})
	tr.handle(agent.Event{Kind: agent.EventText, Text: "Partial reply"})
	tr.handle(agent.Event{Kind: agent.EventThinking, Text: "More"})
	tr.close()
	tr.handle(agent.Event{Kind: agent.EventText, Text: " ignored"})
	tr.handle(agent.Event{Kind: agent.EventThinking, Text: "ignored"})
	tr.endTurn()

	chat, err := w.Chat("plan")
	if err != nil {
		t.Fatal(err)
	}
	if len(chat) != 1 || chat[0].Text != "Partial reply" {
		t.Fatalf("chat = %+v, want only saved partial reply", chat)
	}
	got := shown()
	if last := got[len(got)-1]; last.Thinking != "" || last.Preparing != "" {
		t.Fatalf("last working = %+v, want cleared", last)
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
