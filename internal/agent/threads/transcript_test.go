package threads

import (
	"testing"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/mcp"
)

// Our own tools show in the chat as their entry in mcp's tool table says,
// whatever the agent titles them; other servers' tools aren't ours to hide.
func TestOwnToolsShowInTheChatAsListed(t *testing.T) {
	call := func(server, tool string) agent.Tool {
		return agent.Tool{Kind: agent.ToolOther, Title: "Called " + tool, MCPServer: server, MCPTool: tool}
	}
	if label, ok := toolLabel(call(mcp.Name, "ask_user")); ok {
		t.Errorf("ask_user shows as %q", label)
	}
	if label, ok := toolLabel(call(mcp.Name, "set_toolchain")); !ok || label != "tutortop · set_toolchain" {
		t.Errorf("set_toolchain shows as %q, %v", label, ok)
	}
	if _, ok := toolLabel(call("other", "ask_user")); !ok {
		t.Error("another server's ask_user is hidden")
	}
}
