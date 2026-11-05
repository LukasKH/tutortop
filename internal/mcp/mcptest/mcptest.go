// Package mcptest helps test code that serves or calls the mcp package's
// tools: links that open without the network, and a client that calls a
// thread's tools the way its agent does.
package mcptest

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Links is a LinkChecker that finds every page but the missing ones, which
// answer 404, without the network.
func Links(missing ...string) *mcp.LinkChecker {
	return mcp.NewLinkChecker(roundTripper(func(r *http.Request) (*http.Response, error) {
		status := http.StatusOK
		if slices.Contains(missing, r.URL.String()) {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Status: fmt.Sprint(status, " ", http.StatusText(status)), Body: http.NoBody, Request: r}, nil
	}))
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Caller connects to one thread's MCP tools at url, the way that thread's
// agent would, and returns a function that calls a tool and reports its text
// and whether it was an error.
func Caller(t *testing.T, url string) func(name string, args any) (string, bool) {
	t.Helper()
	cs := Session(t, url)
	return func(name string, args any) (string, bool) {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcpsdk.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return b.String(), res.IsError
	}
}

// Session connects to one thread's MCP server at url over HTTP, as its agent
// does.
func Session(t *testing.T, url string) *mcpsdk.ClientSession {
	t.Helper()
	cs, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil).
		Connect(context.Background(), &mcpsdk.StreamableClientTransport{Endpoint: url}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}
