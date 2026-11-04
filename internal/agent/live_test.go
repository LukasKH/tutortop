//go:build live

// Live tests talk to the real, signed-in CLIs and use a little quota.
// Run with: go test -tags live -run TestLive -v ./internal/agent/
package agent

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLive(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			url, called := probeMCP(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			var mu sync.Mutex
			var text strings.Builder
			var tools []string
			sess, err := Start(ctx, provider, Options{
				Cwd: t.TempDir(), MCPName: "probe", MCPURL: url,
				Instructions: "You are being tested by an automated harness. Follow instructions exactly and be brief.",
			}, func(ev Event) {
				mu.Lock()
				defer mu.Unlock()
				switch ev.Kind {
				case EventText:
					text.WriteString(ev.Text)
				case EventTool:
					tools = append(tools, ev.Tool.Status+": "+ev.Tool.Title)
				}
			})
			if err != nil && strings.Contains(err.Error(), "not signed in") {
				t.Skip(err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()

			if err := sess.Prompt(ctx, "Call the ping tool on the probe MCP server, then reply with only the secret word it returns."); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			t.Logf("session=%s tools=%v text=%q", sess.ID(), tools, text.String())
			if !*called {
				t.Error("the MCP tool was never called")
			}
			if !strings.Contains(text.String(), "TURTLE") {
				t.Error("reply doesn't contain the secret word")
			}
			if sess.ID() == "" {
				t.Error("no session id")
			}
		})
	}
}

func probeMCP(t *testing.T) (url string, called *bool) {
	called = new(bool)
	s := mcp.NewServer(&mcp.Implementation{Name: "probe", Version: "1"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "ping", Description: "Returns the secret word."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			*called = true
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "secret word: TURTLE"}}}, nil, nil
		})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String() + "/mcp", called
}
