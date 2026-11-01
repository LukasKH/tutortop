package app

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/textproto"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/runner"
)

// Language servers give the editor completions, hover docs and errors. Each
// open task gets one, running in the task's sandbox image. The app only
// relays protocol messages: the frontend speaks LSP, we frame it over stdio.

// maxLanguageServers bounds how many run at once; starting another stops the
// oldest. The UI shows one task at a time, so this only matters when a
// reloaded window leaves its old server behind.
const maxLanguageServers = 2

type languageServer struct {
	id      string
	key     threadKey
	srv     *runner.Server
	started time.Time

	writeMu sync.Mutex
}

// LanguageServerInfo tells the editor how to talk to a started server.
type LanguageServerInfo struct {
	ID string `json:"id"`
	// RootURI is the task folder as the server sees it, e.g. file:///work.
	RootURI string `json:"rootUri"`
}

// lspEvent carries one message from a server to the editor, or tells it the
// server has stopped.
type lspEvent struct {
	ID      string `json:"id"`
	Message string `json:"message,omitempty"`
	Closed  bool   `json:"closed,omitempty"`
}

// StartLanguageServer starts a language server for a task with the learner's
// current code and the visible tests. It returns an empty ID if the task's
// language has none. The first start may build the sandbox image.
func (a *App) StartLanguageServer(wsID, taskID string) (LanguageServerInfo, error) {
	w, err := a.store.Open(wsID)
	if err != nil {
		return LanguageServerInfo{}, err
	}
	d, err := w.Task(taskID)
	if err != nil {
		return LanguageServerInfo{}, err
	}
	tc, err := w.Toolchain(d.Language)
	if err != nil || len(tc.LanguageServer) == 0 {
		return LanguageServerInfo{}, err
	}
	files := maps.Clone(d.Tests)
	maps.Copy(files, d.Work)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	srv, err := a.runner.StartLanguageServer(ctx, tc, files)
	if err != nil {
		return LanguageServerInfo{}, err
	}
	ls := &languageServer{id: rand.Text(), key: threadKey{wsID, taskID}, srv: srv, started: time.Now()}

	a.mu.Lock()
	var stale []*languageServer
	for _, other := range a.servers {
		if other.key == ls.key {
			stale = append(stale, other)
		}
	}
	for len(a.servers)-len(stale) >= maxLanguageServers {
		stale = append(stale, a.oldestServer(stale))
	}
	for _, s := range stale {
		delete(a.servers, s.id)
	}
	a.servers[ls.id] = ls
	a.mu.Unlock()
	for _, s := range stale {
		s.srv.Close()
	}

	go a.relay(wsID, ls)
	return LanguageServerInfo{ID: ls.id, RootURI: "file://" + srv.Root}, nil
}

// oldestServer returns the longest-running server not already in skip.
// Callers hold a.mu.
func (a *App) oldestServer(skip []*languageServer) *languageServer {
	var oldest *languageServer
	for _, s := range a.servers {
		if !slices.Contains(skip, s) && (oldest == nil || s.started.Before(oldest.started)) {
			oldest = s
		}
	}
	return oldest
}

// relay forwards every message from the server to the editor until it stops.
func (a *App) relay(wsID string, ls *languageServer) {
	r := bufio.NewReader(ls.srv.Stdout)
	for {
		msg, err := readMessage(r)
		if err != nil {
			break
		}
		a.emit(wsID, "lsp", lspEvent{ID: ls.id, Message: string(msg)})
	}
	a.mu.Lock()
	delete(a.servers, ls.id)
	a.mu.Unlock()
	ls.srv.Close()
	a.emit(wsID, "lsp", lspEvent{ID: ls.id, Closed: true})
}

// SendLanguageServer sends one JSON-RPC message from the editor to a server.
func (a *App) SendLanguageServer(id, message string) error {
	a.mu.Lock()
	ls := a.servers[id]
	a.mu.Unlock()
	if ls == nil {
		return errors.New("the language server has stopped")
	}
	ls.writeMu.Lock()
	defer ls.writeMu.Unlock()
	return writeMessage(ls.srv.Stdin, []byte(message))
}

// StopLanguageServer stops a server the editor no longer needs.
func (a *App) StopLanguageServer(id string) {
	a.mu.Lock()
	ls := a.servers[id]
	delete(a.servers, id)
	a.mu.Unlock()
	if ls != nil {
		ls.srv.Close()
	}
}

// ---- LSP framing: a Content-Length header, a blank line, then JSON ----

const maxMessage = 64 << 20

func readMessage(r *bufio.Reader) ([]byte, error) {
	header, err := textproto.NewReader(r).ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(header.Get("Content-Length"))
	if err != nil || n < 0 || n > maxMessage {
		return nil, fmt.Errorf("bad Content-Length %q", header.Get("Content-Length"))
	}
	body := make([]byte, n)
	_, err = io.ReadFull(r, body)
	return body, err
}

func writeMessage(w io.Writer, body []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

// probeLanguageServer checks that a server answers the LSP initialize request,
// so a broken language_server command is caught when the coach sets it up.
func (a *App) probeLanguageServer(ctx context.Context, tc runner.Toolchain, files runner.Files) error {
	srv, err := a.runner.StartLanguageServer(ctx, tc, files)
	if err != nil {
		return err
	}
	defer srv.Close()
	init, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"processId": nil, "rootUri": "file://" + srv.Root, "capabilities": map[string]any{}},
	})
	if err := writeMessage(srv.Stdin, init); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		r := bufio.NewReader(srv.Stdout)
		for {
			msg, err := readMessage(r)
			if err != nil {
				done <- fmt.Errorf("the language server stopped before answering initialize: %w", err)
				return
			}
			var resp struct {
				ID    json.RawMessage `json:"id"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
				Result json.RawMessage `json:"result"`
			}
			if json.Unmarshal(msg, &resp) == nil && string(resp.ID) == "1" && (resp.Result != nil || resp.Error != nil) {
				if resp.Error != nil {
					done <- errors.New("initialize failed: " + resp.Error.Message)
				} else {
					done <- nil
				}
				return
			}
		}
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(90 * time.Second):
		return errors.New("the language server didn't answer initialize within 90 seconds")
	case <-ctx.Done():
		return ctx.Err()
	}
}
