package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tutor changes the app's state only through the MCP tools served here.
// newServer lists which thread gets which tool. Each tool is an *mcp.Tool
// (its name, and a description the agent reads as instructions), a method on
// threadTools that handles it, and its input and output types, kept together:
// get_state below, the chat tools in mcp_chat.go, the writer tools in
// mcp_writer.go.
//
// To add a tool, write those three next to the others, then add it to
// newServer under the threads that need it. The SDK derives the input schema
// from the input type's json and jsonschema tags and rejects calls that don't
// match it, so the handler only checks what the schema can't say.

// mcpHost serves one MCP server per chat thread on localhost. Each gets a
// random token in its URL, so an agent can only reach its own workspace, and
// tools like ask_user know which conversation called them.
type mcpHost struct {
	app     *App
	baseURL string

	mu      sync.Mutex
	servers map[string]*mcp.Server // token -> server
	tokens  map[threadKey]string
}

func startMCP(a *App) (*mcpHost, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	h := &mcpHost{app: a, baseURL: "http://" + ln.Addr().String(), servers: map[string]*mcp.Server{}, tokens: map[threadKey]string{}}
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		token := strings.TrimPrefix(r.URL.Path, "/mcp/")
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.servers[token] // nil -> 400
	}, nil)
	go http.Serve(ln, handler)
	return h, nil
}

// URL returns the MCP endpoint for one thread, creating its server on first use.
func (h *mcpHost) URL(w *workspace.Workspace, thread workspace.Thread) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := threadKey{w.ID, thread.String()}
	token, ok := h.tokens[key]
	if !ok {
		token = rand.Text()
		h.tokens[key] = token
		h.servers[token] = h.newServer(w, thread)
	}
	return h.baseURL + "/mcp/" + token
}

// forget drops the MCP servers of a deleted workspace.
func (h *mcpHost) forget(wsID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, token := range h.tokens {
		if key.ws == wsID {
			delete(h.tokens, key)
			delete(h.servers, token)
		}
	}
}

// newServer builds the tools for one thread.
func (h *mcpHost) newServer(w *workspace.Workspace, thread workspace.Thread) *mcp.Server {
	t := &threadTools{app: h.app, w: w, thread: thread}
	s := mcp.NewServer(&mcp.Implementation{Name: "tutortop", Version: "0.1.1"}, nil)
	mcp.AddTool(s, getStateTool, t.getState)

	// Only an objective's writer publishes its lesson and task, so the
	// tutoring and planning chats stay about the learner.
	if thread.Kind == workspace.WriterKind {
		mcp.AddTool(s, createTaskTool, t.createTask)
		mcp.AddTool(s, createLessonTool, t.createLesson)
		return s
	}

	// Chats (the plan, a task's or a lesson's) talk with the learner and
	// shape the plan.
	mcp.AddTool(s, askUserTool, t.askUser)
	mcp.AddTool(s, setIconTool, t.setIcon)
	mcp.AddTool(s, proposeCurriculumTool, t.proposeCurriculum)
	mcp.AddTool(s, setToolchainTool, t.setToolchain)
	return s
}

// threadTools handles the tool calls of one thread.
//
// A handler that returns an error doesn't fail the call: the SDK sends the
// error's text back as an error result, which the agent reads and acts on. So
// errors here are written for the agent, and say what to do next.
type threadTools struct {
	app    *App
	w      *workspace.Workspace
	thread workspace.Thread
}

var getStateTool = &mcp.Tool{
	Name:        "get_state",
	Description: "Mission, notes, curriculum, pending proposal, derived progress, tasks and recent attempts for this workspace.",
}

type stateOutput struct {
	Icon       string                   `json:"icon" jsonschema:"the workspace's icon; empty until you pick one with set_icon"`
	Mission    string                   `json:"mission"`
	Notes      string                   `json:"notes"`
	Curriculum *workspace.Curriculum    `json:"curriculum"`
	Proposal   *workspace.Curriculum    `json:"pendingProposal"`
	Progress   workspace.Progress       `json:"progress"`
	Tasks      []workspace.TaskStatus   `json:"tasks"`
	Lessons    []workspace.LessonStatus `json:"lessons" jsonschema:"lessons with the learner's quiz results: answered and correct (first tries)"`
	Recent     []workspace.Attempt      `json:"recentAttempts"`
	Toolchains []runner.ToolchainSpec   `json:"toolchains" jsonschema:"languages set up with set_toolchain, besides the built-in go and python"`
}

func (t *threadTools) getState(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, stateOutput, error) {
	w := t.w
	out := stateOutput{Icon: w.Meta.Icon, Mission: w.ReadDoc("MISSION.md"), Notes: w.ReadDoc("NOTES.md"), Progress: w.Progress()}
	out.Curriculum, _ = w.Curriculum()
	out.Proposal, _ = w.Proposal()
	out.Tasks, _ = w.TaskStatuses()
	out.Lessons, _ = w.LessonStatuses()
	attempts, _ := w.Attempts()
	out.Recent = attempts[max(0, len(attempts)-20):]
	out.Toolchains = w.ToolchainSpecs()
	return nil, out, nil
}

// textResult is a tool's reply to the agent.
func textResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}

// fixAndRetry sends the agent back the problems with its call to tool.
func fixAndRetry(tool *mcp.Tool, problems error) error {
	return fmt.Errorf("%w\nFix these and call %s again", problems, tool.Name)
}

// sandboxProblem reports a sandbox that doesn't work, which the agent can't fix.
func sandboxProblem(err error) error {
	return fmt.Errorf("sandbox problem (not your fault, tell the learner): %w", err)
}
