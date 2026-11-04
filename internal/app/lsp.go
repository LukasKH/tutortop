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

	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// EditorService serves the code editor: language servers give it completions,
// hover docs and errors. Each open task gets one, running in the task's
// sandbox image. The app only relays protocol messages: the frontend speaks
// LSP, we frame it over stdio. It stops the servers as the app quits.
type EditorService struct {
	store   *workspace.Store
	runner  *sandbox.Runner
	servers *languageServers
}

var _ application.ServiceShutdown = (*EditorService)(nil)

func newEditorService(store *workspace.Store, runner *sandbox.Runner, servers *languageServers) *EditorService {
	return &EditorService{store: store, runner: runner, servers: servers}
}

// ServiceShutdown is called by Wails on exit and stops the language servers.
func (s *EditorService) ServiceShutdown() error {
	s.servers.stopAll()
	return nil
}

// LanguageServerInfo tells the editor how to talk to a started server.
type LanguageServerInfo struct {
	ID string `json:"id"`
	// RootURI is the task folder as the server sees it, e.g. file:///work.
	RootURI string `json:"rootUri"`
}

// StartLanguageServer starts a language server for a task with the learner's
// current code and the visible tests. It returns an empty ID if the task's
// language has none. The first start may build the sandbox image.
func (s *EditorService) StartLanguageServer(wsID, taskID string) (LanguageServerInfo, error) {
	w, err := s.store.Open(wsID)
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
	srv, err := s.runner.StartLanguageServer(ctx, tc, files)
	if err != nil {
		return LanguageServerInfo{}, err
	}
	ls := &languageServer{id: rand.Text(), key: taskKey{wsID, taskID}, srv: srv, started: time.Now()}

	s.servers.add(ls)
	go s.servers.relay(ls)
	return LanguageServerInfo{ID: ls.id, RootURI: "file://" + srv.Root}, nil
}

// SendLanguageServer sends one JSON-RPC message from the editor to a server.
func (s *EditorService) SendLanguageServer(id, message string) error {
	ls := s.servers.get(id)
	if ls == nil {
		return errors.New("the language server has stopped")
	}
	ls.writeMu.Lock()
	defer ls.writeMu.Unlock()
	return writeMessage(ls.srv.Stdin, []byte(message))
}

// StopLanguageServer stops a server the editor no longer needs.
func (s *EditorService) StopLanguageServer(id string) { s.servers.stop(id) }

// languageServers are the running language servers, by id.
type languageServers struct {
	mu      sync.Mutex
	servers map[string]*languageServer
}

type languageServer struct {
	id      string
	key     taskKey // the task whose code it serves
	srv     *sandbox.Server
	started time.Time

	writeMu sync.Mutex
}

// taskKey names one task of one workspace.
type taskKey struct{ ws, task string }

func newLanguageServers() *languageServers {
	return &languageServers{servers: map[string]*languageServer{}}
}

// maxLanguageServers bounds how many run at once; starting another stops the
// oldest. The UI shows one task at a time, so this only matters when a
// reloaded window leaves its old server behind.
const maxLanguageServers = 2

// add keeps a started server. It stops the task's other servers, and the
// oldest ones beyond maxLanguageServers.
func (r *languageServers) add(ls *languageServer) {
	r.mu.Lock()
	var stale []*languageServer
	for _, other := range r.servers {
		if other.key == ls.key {
			stale = append(stale, other)
		}
	}
	for len(r.servers)-len(stale) >= maxLanguageServers {
		stale = append(stale, r.oldest(stale))
	}
	for _, s := range stale {
		delete(r.servers, s.id)
	}
	r.servers[ls.id] = ls
	r.mu.Unlock()
	for _, s := range stale {
		s.srv.Close()
	}
}

// oldest returns the longest-running server not already in skip. Call with
// r.mu held.
func (r *languageServers) oldest(skip []*languageServer) *languageServer {
	var oldest *languageServer
	for _, s := range r.servers {
		if !slices.Contains(skip, s) && (oldest == nil || s.started.Before(oldest.started)) {
			oldest = s
		}
	}
	return oldest
}

// get returns a running server, or nil if it has stopped.
func (r *languageServers) get(id string) *languageServer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.servers[id]
}

// stop stops a server, if it's still running.
func (r *languageServers) stop(id string) {
	r.mu.Lock()
	ls := r.servers[id]
	delete(r.servers, id)
	r.mu.Unlock()
	if ls != nil {
		ls.srv.Close()
	}
}

// stopWorkspace stops a workspace's servers, before it's deleted.
func (r *languageServers) stopWorkspace(wsID string) {
	r.mu.Lock()
	var servers []*languageServer
	for id, ls := range r.servers {
		if ls.key.ws == wsID {
			delete(r.servers, id)
			servers = append(servers, ls)
		}
	}
	r.mu.Unlock()
	for _, ls := range servers {
		ls.srv.Close()
	}
}

// stopAll stops every server, as the app quits.
func (r *languageServers) stopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ls := range r.servers {
		ls.srv.Close()
	}
}

// relay forwards every message from the server to the editor until it stops.
func (r *languageServers) relay(ls *languageServer) {
	br := bufio.NewReader(ls.srv.Stdout)
	for {
		msg, err := readMessage(br)
		if err != nil {
			break
		}
		events.Emit(events.LSPMessage, events.LSP{ID: ls.id, Message: string(msg)})
	}
	r.mu.Lock()
	delete(r.servers, ls.id)
	r.mu.Unlock()
	ls.srv.Close()
	events.Emit(events.LSPMessage, events.LSP{ID: ls.id, Closed: true})
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
// so a broken language_server command is caught when the tutor sets it up.
func probeLanguageServer(ctx context.Context, runner *sandbox.Runner, tc sandbox.Toolchain, files sandbox.Files) error {
	srv, err := runner.StartLanguageServer(ctx, tc, files)
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
			resp, err := decode.JSON[lspResponse](msg)
			if err == nil && string(resp.ID) == "1" && (resp.Result != nil || resp.Error != nil) {
				if resp.Error != nil {
					done <- errors.New("initialize failed: " + resp.Error.Message)
				} else {
					done <- nil
				}
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeoutCause(ctx, 90*time.Second, errors.New("the language server didn't answer initialize within 90 seconds"))
	defer cancel()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// lspResponse is the part of a JSON-RPC response probeLanguageServer reads.
type lspResponse struct {
	ID    json.RawMessage `json:"id"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Result json.RawMessage `json:"result"`
}
