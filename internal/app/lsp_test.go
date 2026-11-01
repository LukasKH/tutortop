package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/runner"
)

func TestMessageFraming(t *testing.T) {
	var buf bytes.Buffer
	for _, m := range []string{`{"a":1}`, `{"b":"ü"}`} {
		if err := writeMessage(&buf, []byte(m)); err != nil {
			t.Fatal(err)
		}
	}
	r := bufio.NewReader(&buf)
	for _, want := range []string{`{"a":1}`, `{"b":"ü"}`} {
		got, err := readMessage(r)
		if err != nil || string(got) != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
}

func sandboxApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t, nil)
	if err := a.runner.Check(context.Background()); err != nil {
		t.Skip(err)
	}
	if a.runner.Engine == "local" {
		t.Skip("language servers are tested in the sandbox")
	}
	return a
}

func TestBuiltinLanguageServersAnswer(t *testing.T) {
	a := sandboxApp(t)
	for lang, files := range map[string]runner.Files{
		"go":     {"solution.go": "package solution\n"},
		"python": {"solution.py": "def f():\n    pass\n"},
	} {
		t.Run(lang, func(t *testing.T) {
			tc, _ := runner.Builtin(lang)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			if err := a.probeLanguageServer(ctx, tc, files); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestGoplsCompletes checks the whole path the editor uses: task files reach
// the container, and gopls answers a completion request with real results.
func TestGoplsCompletes(t *testing.T) {
	a := sandboxApp(t)
	tc, _ := runner.Builtin("go")
	src := "package solution\n\nimport \"strings\"\n\nfunc F(s string) []string { return strings.Fi }\n"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	srv, err := a.runner.StartLanguageServer(ctx, tc, runner.Files{"solution.go": src})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	uri := "file://" + srv.Root + "/solution.go"
	send := func(m map[string]any) {
		m["jsonrpc"] = "2.0"
		b, _ := json.Marshal(m)
		if err := writeMessage(srv.Stdin, b); err != nil {
			t.Fatal(err)
		}
	}
	send(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"processId": nil, "rootUri": "file://" + srv.Root, "capabilities": map[string]any{}}})
	send(map[string]any{"method": "initialized", "params": map[string]any{}})
	send(map[string]any{"method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "go", "version": 1, "text": src}}})
	col := strings.Index(strings.Split(src, "\n")[4], "strings.Fi") + len("strings.Fi")
	send(map[string]any{"id": 2, "method": "textDocument/completion", "params": map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 4, "character": col}}})

	done := make(chan string, 1)
	go func() {
		r := bufio.NewReader(srv.Stdout)
		for {
			msg, err := readMessage(r)
			if err != nil {
				done <- "error: " + err.Error()
				return
			}
			var m struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
			}
			_ = json.Unmarshal(msg, &m)
			switch {
			case m.Method != "" && m.ID != nil: // a request from the server: answer it
				send(map[string]any{"id": m.ID, "result": nil})
			case string(m.ID) == "2":
				done <- string(m.Result)
				return
			}
		}
	}()
	select {
	case got := <-done:
		if !strings.Contains(got, `"Fields"`) {
			t.Fatalf("completion result doesn't offer strings.Fields: %.500s", got)
		}
	case <-time.After(2 * time.Minute):
		t.Fatal("no completion answer")
	}
}
