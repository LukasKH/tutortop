package app

import (
	"testing"

	"github.com/LukasKH/tutortop/internal/agent"
)

func TestToolLabel(t *testing.T) {
	for _, c := range []struct {
		tool agent.Tool
		want string // "" means hidden
	}{
		{agent.Tool{Kind: agent.ToolRead, Title: "Read NOTES.md"}, ""},
		{agent.Tool{Kind: agent.ToolThink, Title: "TodoWrite"}, ""},
		{agent.Tool{Kind: agent.ToolEdit, Title: "Edited MISSION.md", Paths: []string{"/ws/MISSION.md"}}, "Updated your mission"},
		{agent.Tool{Kind: agent.ToolEdit, Title: "Edited 0003-errors.md", Paths: []string{"learning-records/0003-errors.md"}}, "Recorded what you learned"},
		{agent.Tool{Kind: agent.ToolEdit, Title: "Edited a.md, b.md", Paths: []string{"a.md", "b.md"}}, "Edited a.md, b.md"},
		{agent.Tool{Kind: agent.ToolOther, Title: "tutortop · create_task", MCPServer: "tutortop", MCPTool: "create_task"}, ""},
		{agent.Tool{Kind: agent.ToolOther, Title: "tutortop · run_tests", MCPServer: "tutortop", MCPTool: "run_tests"}, "tutortop · run_tests"},
		{agent.Tool{Kind: agent.ToolFetch, Title: "Searched the web: goroutines"}, "Searched the web: goroutines"},
		{agent.Tool{ID: "x", Status: agent.ToolDone}, ""},
	} {
		got, show := toolLabel(c.tool)
		if show != (c.want != "") || got != c.want {
			t.Errorf("toolLabel(%+v) = %q, %v; want %q", c.tool, got, show, c.want)
		}
	}
}
