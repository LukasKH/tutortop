// Package mcptest helps test code that serves or calls the mcp package's
// tools: links that open without the network, a client that calls a thread's
// tools the way its agent does, and a Go task that passes the quality gate.
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

// ReverseWordsTask is create_task's input for a Go task for objective that
// passes the quality gate.
func ReverseWordsTask(objective string) map[string]any {
	return map[string]any{
		"objectiveId": objective,
		"title":       "Reverse words",
		"statement": map[string]any{
			"task":         "Write ReverseWords, which returns the words of a sentence in reverse order.",
			"context":      "Command-line tools often reorder user input.",
			"signature":    "func ReverseWords(s string) string",
			"requirements": []string{"Words are separated by runs of spaces.", "Join the result with single spaces."},
			"examples": []any{
				map[string]any{"input": `ReverseWords("hello world")`, "output": `"world hello"`},
				map[string]any{"input": `ReverseWords("  a   b ")`, "output": `"b a"`, "explanation": "Extra spaces don't make empty words."},
			},
		},
		"starter":     map[string]string{"solution.go": GoStarter},
		"reference":   map[string]string{"solution.go": GoReference},
		"tests":       map[string]string{"visible_test.go": goVisible},
		"hiddenTests": map[string]string{"hidden_test.go": goHidden},
		"mutants": []any{
			map[string]any{"name": "split", "description": "keeps empty words", "files": map[string]string{"solution.go": strings.Replace(GoReference, "strings.Fields(s)", `strings.Split(s, " ")`, 1)}},
			map[string]any{"name": "no-reverse", "description": "forgets to reverse", "files": map[string]string{"solution.go": "package solution\n\nimport \"strings\"\n\nfunc ReverseWords(s string) string { return strings.Join(strings.Fields(s), \" \") }\n"}},
		},
	}
}

// GoReference is ReverseWordsTask's reference solution, which passes every
// test.
const GoReference = `package solution

import "strings"

func ReverseWords(s string) string {
	words := strings.Fields(s)
	for i, j := 0, len(words)-1; i < j; i, j = i+1, j-1 {
		words[i], words[j] = words[j], words[i]
	}
	return strings.Join(words, " ")
}
`

// GoStarter is ReverseWordsTask's starter code, which fails the tests.
const GoStarter = "package solution\n\nfunc ReverseWords(s string) string {\n\treturn \"\"\n}\n"

const goVisible = `package solution

import "testing"

func TestBasic(t *testing.T) {
	if got := ReverseWords("hello world"); got != "world hello" {
		t.Fatalf("got %q", got)
	}
}
`

const goHidden = `package solution

import "testing"

func TestExtraSpaces(t *testing.T) {
	if got := ReverseWords("  a   b  "); got != "b a" {
		t.Fatalf("got %q", got)
	}
}

func TestEmpty(t *testing.T) {
	if got := ReverseWords(""); got != "" {
		t.Fatalf("got %q", got)
	}
}
`
