// Package mcp serves the tools agents change the app's state through, one MCP
// server per thread. It knows the workspaces and the sandbox, not the agents
// that call it: a thread's agent hands it Checks to show its sandbox checks.
//
// tools (tools.go) lists which kind of thread gets which tool. Each area's
// tools live in their own file; docs/architecture.md ("MCP tools") has the
// steps for adding one.
package mcp

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Checks shows the learner how a sandbox check is going.
type Checks interface {
	Show(wsID string, c events.SandboxCheck)
}

// Name is the name agents know Tutortop's MCP server by.
const Name = "tutortop"

// Server serves one MCP server per thread, chats and writers alike, on
// localhost. Each gets a random token in its URL, so an agent can only reach
// its own workspace, and tools like ask_user know which thread called them.
type Server struct {
	runner  *sandbox.Runner // sandbox checks: toolchains and the quality gate
	links   *LinkChecker    // the references a plan or lesson cites
	baseURL string

	mu      sync.Mutex
	servers map[string]*mcpsdk.Server // token -> server
	tokens  map[threadKey]string
}

// Start serves the threads' tools on a free port on localhost. They run
// sandbox checks on runner and check the links agents cite with links.
func Start(runner *sandbox.Runner, links *LinkChecker) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{
		runner:  runner,
		links:   links,
		baseURL: "http://" + ln.Addr().String(),
		servers: map[string]*mcpsdk.Server{},
		tokens:  map[threadKey]string{},
	}
	handler := mcpsdk.NewStreamableHTTPHandler(func(r *http.Request) *mcpsdk.Server {
		token := strings.TrimPrefix(r.URL.Path, "/mcp/")
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.servers[token] // nil -> 400
	}, nil)
	go func() {
		// Serve returns only if the listener fails. The agents then lose their tools.
		slog.Error("MCP server stopped", "err", http.Serve(ln, handler))
	}()
	return s, nil
}

// URL returns the MCP endpoint for one thread, creating its server on first
// use. checks shows the thread's sandbox checks; it's the thread's agent,
// which needs this URL to start.
func (s *Server) URL(w *workspace.Workspace, thread workspace.Thread, checks Checks) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadKey{w.ID, thread.String()}
	token, ok := s.tokens[key]
	if !ok {
		token = rand.Text()
		s.tokens[key] = token
		s.servers[token] = s.newServer(w, thread, checks)
	}
	return s.baseURL + "/mcp/" + token
}

// Forget drops the MCP servers of a deleted workspace.
func (s *Server) Forget(wsID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, token := range s.tokens {
		if key.ws == wsID {
			delete(s.tokens, key)
			delete(s.servers, token)
		}
	}
}

// newServer builds one thread's server with the tools of its kind.
func (s *Server) newServer(w *workspace.Workspace, thread workspace.Thread, checks Checks) *mcpsdk.Server {
	t := &threadTools{w: w, thread: thread, checks: checks, runner: s.runner, links: s.links}
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: Name, Version: "0.1.1"}, nil)
	for _, tool := range toolsFor(thread.Kind) {
		tool.add(srv, t)
	}
	return srv
}

// threadKey names one thread of one workspace by the thread's name.
type threadKey struct{ ws, name string }

// threadTools handles the tool calls of one thread.
//
// A handler that returns an error doesn't fail the call: the SDK sends the
// error's text back as an error result, which the agent reads and acts on. So
// errors here are written for the agent, and say what to do next.
type threadTools struct {
	w      *workspace.Workspace
	thread workspace.Thread
	checks Checks // show the thread's sandbox checks
	runner *sandbox.Runner
	links  *LinkChecker
}

// ownObjective refuses a writer's work on another objective, which has its
// own writer.
func (t *threadTools) ownObjective(id string) error {
	if id != t.thread.ID {
		return fmt.Errorf("you write for the objective %q only, not %q", t.thread.ID, id)
	}
	return nil
}

// textResult is a tool's reply to the agent.
func textResult(msg string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: msg}}}
}

// fixAndRetry sends the agent back the problems with its call to tool.
func fixAndRetry(tool *mcpsdk.Tool, problems error) error {
	return fmt.Errorf("%w\nFix these and call %s again", problems, tool.Name)
}

// sandboxProblem reports a sandbox that doesn't work, which the agent can't fix.
func sandboxProblem(err error) error {
	return fmt.Errorf("sandbox problem (not your fault; tell the learner in plain words, without naming internal tools or files): %w", err)
}

// progressInterval is how often a slow tool call notifies its MCP client that
// it is still working, so clients with an idle timeout don't abort a slow
// image build or quality gate. Overridden in tests.
var progressInterval = 15 * time.Second

// keepAlive sends periodic MCP progress notifications while a slow tool call
// runs, when the client provided a progressToken. Call the returned function
// before returning from the tool handler.
func keepAlive(ctx context.Context, req *mcpsdk.CallToolRequest, message string) func() {
	if req == nil || req.Session == nil {
		return func() {}
	}
	token := req.Params.GetProgressToken()
	if token == nil {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(progressInterval)
		defer tick.Stop()
		for n := 1.0; ; n++ {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				// Best-effort heartbeat; the tool call itself reports real errors.
				_ = req.Session.NotifyProgress(ctx, &mcpsdk.ProgressNotificationParams{
					ProgressToken: token,
					Progress:      n,
					Message:       message,
				})
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}
