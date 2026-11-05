// Package editor serves the code editor's language servers (Service): each
// open task gets one, running in the task's sandbox image, and the app
// relays its protocol messages to and from the frontend.
package editor

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Service serves the editor its language servers, and stops them as the app
// quits. The frontend speaks LSP; Service frames it over stdio.
type Service struct {
	store   *workspace.Store
	runner  *sandbox.Runner
	servers *Servers
}

var _ application.ServiceShutdown = (*Service)(nil)

// NewService serves the editor the workspaces in store, starting language
// servers on runner and keeping them in servers.
func NewService(store *workspace.Store, runner *sandbox.Runner, servers *Servers) *Service {
	return &Service{store: store, runner: runner, servers: servers}
}

// ServiceShutdown is called by Wails on exit and stops the language servers.
func (s *Service) ServiceShutdown() error {
	s.servers.stopAll()
	return nil
}

// StartLanguageServer starts a language server for a task with the learner's
// current code and the visible tests. It returns an empty ID if the task's
// language has none. The first start may build the sandbox image.
func (s *Service) StartLanguageServer(wsID, taskID string) (LanguageServerInfo, error) {
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
func (s *Service) SendLanguageServer(id, message string) error {
	ls := s.servers.get(id)
	if ls == nil {
		return errors.New("the language server has stopped")
	}
	ls.writeMu.Lock()
	defer ls.writeMu.Unlock()
	return sandbox.WriteMessage(ls.srv.Stdin, []byte(message))
}

// StopLanguageServer stops a server the editor no longer needs.
func (s *Service) StopLanguageServer(id string) { s.servers.stop(id) }

// LanguageServerInfo tells the editor how to talk to a started server.
type LanguageServerInfo struct {
	ID string `json:"id"`
	// RootURI is the task folder as the server sees it, e.g. file:///work.
	RootURI string `json:"rootUri"`
}

// Servers are the running language servers, by id. The editor's Service
// starts them; a workspace's are stopped before it's deleted.
type Servers struct {
	mu      sync.Mutex
	servers map[string]*languageServer
}

// NewServers keeps no servers yet.
func NewServers() *Servers {
	return &Servers{servers: map[string]*languageServer{}}
}

// StopWorkspace stops a workspace's servers, before it's deleted.
func (r *Servers) StopWorkspace(wsID string) {
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

// add keeps a started server. It stops the task's other servers, and the
// oldest ones beyond maxLanguageServers.
func (r *Servers) add(ls *languageServer) {
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
func (r *Servers) oldest(skip []*languageServer) *languageServer {
	var oldest *languageServer
	for _, s := range r.servers {
		if !slices.Contains(skip, s) && (oldest == nil || s.started.Before(oldest.started)) {
			oldest = s
		}
	}
	return oldest
}

// get returns a running server, or nil if it has stopped.
func (r *Servers) get(id string) *languageServer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.servers[id]
}

// stop stops a server, if it's still running.
func (r *Servers) stop(id string) {
	r.mu.Lock()
	ls := r.servers[id]
	delete(r.servers, id)
	r.mu.Unlock()
	if ls != nil {
		ls.srv.Close()
	}
}

// stopAll stops every server, as the app quits.
func (r *Servers) stopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ls := range r.servers {
		ls.srv.Close()
	}
}

// relay forwards every message from the server to the editor until it stops.
func (r *Servers) relay(ls *languageServer) {
	br := bufio.NewReader(ls.srv.Stdout)
	for {
		msg, err := sandbox.ReadMessage(br)
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

type languageServer struct {
	id      string
	key     taskKey // the task whose code it serves
	srv     *sandbox.Server
	started time.Time

	writeMu sync.Mutex
}

// taskKey names one task of one workspace.
type taskKey struct{ ws, task string }

// maxLanguageServers bounds how many run at once; starting another stops the
// oldest. The UI shows one task at a time, so this only matters when a
// reloaded window leaves its old server behind.
const maxLanguageServers = 2
