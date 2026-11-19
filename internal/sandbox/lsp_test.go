package sandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/decode"
)

// Every built-in language's editor gets diagnostics from its language server,
// running locked down in the language's image with the task's files, as the
// editor's Service starts it. A new built-in language needs a row here.
func TestLanguageServers(t *testing.T) {
	broken := map[string]openFile{
		"go":     {name: "sum.go", text: "package task\n\nfunc Sum(a, b int) int { return \"x\" }\n"},
		"python": {name: "calc.py", text: "def add(a: int, b: int) -> int:\n    return \"x\"\n"},
	}
	for _, lang := range BuiltinLanguages() {
		t.Run(lang, func(t *testing.T) {
			t.Parallel()
			file, ok := broken[lang]
			if !ok {
				t.Fatalf("add a file with a type error in %s, so its editor is tested", lang)
			}
			tc := builtins[lang]
			srv, err := testRunner(t).StartLanguageServer(t.Context(), tc, Files{file.name: file.text})
			if err != nil {
				t.Fatal(err)
			}
			defer srv.Close()

			got := make(chan error, 1)
			go func() { got <- openWithDiagnostics(srv, tc.EditorLanguage, file) }()
			select {
			case err := <-got:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Minute):
				t.Fatal("no diagnostics within 2 minutes")
			}
		})
	}
}

// openFile is a task file the editor opens.
type openFile struct{ name, text string }

// openWithDiagnostics opens file, as the editor does, and waits until the
// server reports a problem in it.
func openWithDiagnostics(srv *LanguageServer, languageID string, file openFile) error {
	uri := "file://" + srv.Root + "/" + file.name
	send := func(msg map[string]any) error {
		msg["jsonrpc"] = "2.0"
		b, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		return WriteMessage(srv.Stdin, b)
	}
	err := send(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{
		"processId": nil,
		"rootUri":   "file://" + srv.Root,
		"capabilities": map[string]any{
			"textDocument": map[string]any{"publishDiagnostics": map[string]any{}},
		},
	}})
	if err != nil {
		return err
	}
	in := bufio.NewReader(srv.Stdout)
	for {
		b, err := ReadMessage(in)
		if err != nil {
			return fmt.Errorf("the language server stopped: %w", err)
		}
		msg, err := decode.JSON[lspMessage](b)
		switch {
		case err != nil:
			continue // params of another shape
		case msg.Method == "" && string(msg.ID) == "1":
			if msg.Error != nil {
				return fmt.Errorf("initialize failed: %s", msg.Error.Message)
			}
			if err := send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
				return err
			}
			err = send(map[string]any{"method": "textDocument/didOpen", "params": map[string]any{
				"textDocument": map[string]any{"uri": uri, "languageId": languageID, "version": 1, "text": file.text},
			}})
			if err != nil {
				return err
			}
		case msg.Method != "" && msg.ID != nil:
			// A request from the server, such as for configuration; an empty
			// answer keeps it going.
			if err := send(map[string]any{"id": msg.ID, "result": nil}); err != nil {
				return err
			}
		case msg.Method == "textDocument/publishDiagnostics" && msg.Params.URI == uri && len(msg.Params.Diagnostics) > 0:
			return nil
		}
	}
}

// lspMessage is the part of a server's message openWithDiagnostics reads.
type lspMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
	Params struct {
		URI         string            `json:"uri"`
		Diagnostics []json.RawMessage `json:"diagnostics"`
	} `json:"params"`
}
