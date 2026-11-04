package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tutor changes the app's state only through the MCP tools served here.
// mcpTools lists which kind of thread gets which tool, and whether its calls
// show in the chat. Each tool is an
// *mcp.Tool (its name, and a description the agent reads as instructions), a
// method on threadTools that handles it, and its input and output types, kept
// together: get_state below, the chat tools in mcp_chat.go, the writer tools
// in mcp_writer.go.
//
// To add a tool, write those three next to the others, then add a line to
// mcpTools that says whether it shows in the chat and which kinds of thread
// get it. The SDK derives the input
// schema from the input type's json and jsonschema tags and rejects calls
// that don't match it, so the handler only checks what the schema can't say.

// mcpHost serves one MCP server per thread, chats and writers alike, on
// localhost. Each gets a random token in its URL, so an agent can only reach
// its own workspace, and tools like ask_user know which thread called them.
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

// newServer builds one thread's server with the tools of its kind.
func (h *mcpHost) newServer(w *workspace.Workspace, thread workspace.Thread) *mcp.Server {
	t := &threadTools{app: h.app, w: w, thread: thread}
	s := mcp.NewServer(&mcp.Implementation{Name: "tutortop", Version: "0.1.1"}, nil)
	for _, tool := range mcpTools {
		if slices.Contains(tool.kinds, thread.Kind) {
			tool.add(s, t)
		}
	}
	return s
}

// mcpTools lists every tool, whether its calls show in the chat, and the
// kinds of thread that get it. Chats (the plan, a task's, a lesson's) talk
// with the learner and shape the plan. Only an objective's writer publishes
// its lesson and task, so the chats stay about the learner.
//
// Most calls are hidden: reading the state is noise, and the others show in
// their own UI (the questions form, the icon, the proposal screen). Writers
// have no chat; the objective's page shows their work.
var mcpTools = []threadTool{
	toolFor(getStateTool, (*threadTools).getState, hidden, workspace.PlanKind, workspace.TaskKind, workspace.LessonKind, workspace.WriterKind),
	toolFor(askUserTool, (*threadTools).askUser, hidden, workspace.PlanKind, workspace.TaskKind, workspace.LessonKind),
	toolFor(setIconTool, (*threadTools).setIcon, hidden, workspace.PlanKind, workspace.TaskKind, workspace.LessonKind),
	toolFor(proposeCurriculumTool, (*threadTools).proposeCurriculum, hidden, workspace.PlanKind, workspace.TaskKind, workspace.LessonKind),
	toolFor(setToolchainTool, (*threadTools).setToolchain, shown, workspace.PlanKind, workspace.TaskKind, workspace.LessonKind),
	toolFor(createTaskTool, (*threadTools).createTask, hidden, workspace.WriterKind),
	toolFor(createLessonTool, (*threadTools).createLesson, hidden, workspace.WriterKind),
}

// threadTool is one tool, whether it shows in the chat, and the kinds of
// thread that get it.
type threadTool struct {
	name  string
	chat  inChat
	kinds []workspace.ThreadKind
	add   func(*mcp.Server, *threadTools) // adds the tool, handled by one thread's threadTools
}

// inChat is whether a tool's calls show in the chat, under the title the
// agent gives them.
type inChat bool

const (
	shown  inChat = true
	hidden inChat = false
)

func toolFor[In, Out any](tool *mcp.Tool, handle func(*threadTools, context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error), chat inChat, kinds ...workspace.ThreadKind) threadTool {
	return threadTool{name: tool.Name, chat: chat, kinds: kinds, add: func(s *mcp.Server, t *threadTools) {
		mcp.AddTool(s, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			return handle(t, ctx, req, in)
		})
	}}
}

// shownInChat reports whether calls to one of our tools show in the chat.
func shownInChat(name string) bool {
	for _, tool := range mcpTools {
		if tool.name == name {
			return bool(tool.chat)
		}
	}
	return false
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
