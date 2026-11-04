package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/text"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

// URL returns the MCP endpoint for one chat thread, creating its server on first use.
func (h *mcpHost) URL(w *workspace.Workspace, thread string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := threadKey{w.ID, thread}
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

type (
	empty       struct{}
	stateOutput struct {
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
	setIconInput struct {
		Icon string `json:"icon"`
	}
	setToolchainInput struct {
		Toolchain runner.ToolchainSpec `json:"toolchain"`
		Example   toolchainExample     `json:"example" jsonschema:"a tiny solution and test file that must pass, to prove the setup works end to end"`
	}
	toolchainExample struct {
		Solution runner.Files `json:"solution" jsonschema:"filename -> content"`
		Tests    runner.Files `json:"tests" jsonschema:"filename -> content; at least one file matching test_files"`
	}
	createTaskOutput struct {
		Published bool               `json:"published"`
		TaskID    string             `json:"taskId,omitempty"`
		Gate      *runner.GateReport `json:"gate,omitempty"`
		Report    string             `json:"report"`
	}
)

// newServer builds the tools for one thread. A returned error reaches the
// agent as an error result with its text, so it can fix the call.
func (h *mcpHost) newServer(w *workspace.Workspace, thread string) *mcp.Server {
	a := h.app
	s := mcp.NewServer(&mcp.Implementation{Name: "tutortop", Version: "0.1.1"}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_state",
		Description: "Mission, notes, curriculum, pending proposal, derived progress, tasks and recent attempts for this workspace.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, stateOutput, error) {
		out := stateOutput{Icon: w.Meta.Icon, Mission: w.ReadDoc("MISSION.md"), Notes: w.ReadDoc("NOTES.md"), Progress: w.Progress()}
		out.Curriculum, _ = w.Curriculum()
		out.Proposal, _ = w.Proposal()
		out.Tasks, _ = w.TaskStatuses()
		out.Lessons, _ = w.LessonStatuses()
		attempts, _ := w.Attempts()
		out.Recent = attempts[max(0, len(attempts)-20):]
		out.Toolchains = w.ToolchainSpecs()
		return nil, out, nil
	})

	if objective, ok := workspace.ObjectiveOfWriter(thread); ok {
		h.addWriterTools(s, w, objective)
		return s
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ask_user",
		Description: "Show the learner 2-4 focused questions as a form in the app. End your turn after calling this; the answers arrive as the next user message.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in workspace.Questions) (*mcp.CallToolResult, any, error) {
		if len(in.Questions) == 0 || len(in.Questions) > 4 {
			return nil, nil, errors.New("ask 1-4 questions")
		}
		in.Thread = thread
		if err := w.SetQuestions(&in); err != nil {
			return nil, nil, err
		}
		emit(questionsAsked, QuestionsEvent{Workspace: w.ID, Questions: in})
		return textResult("The learner can see the questions now. End your turn; the answers will arrive as the next message."), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "set_icon",
		Description: "Pick the picture the app shows for this workspace on its card, in the curriculum and on lessons. " +
			"Choose the language or tool the workspace centres on, else the glyph closest to the topic. One of: " + strings.Join(workspace.Icons, ", ") + ".",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in setIconInput) (*mcp.CallToolResult, any, error) {
		if err := w.SetIcon(in.Icon); err != nil {
			return nil, nil, err
		}
		emit(workspaceChanged, WorkspaceEvent{Workspace: w.ID})
		return textResult("The workspace shows the " + in.Icon + " icon now."), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "propose_curriculum",
		Description: "Show a curriculum (new or revised) to the learner for approval. Every objective must be verifiable with unit tests. Keep ids of unchanged objectives so progress carries over. End your turn after calling this.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in workspace.Curriculum) (*mcp.CallToolResult, any, error) {
		if err := a.brokenLinks(ctx, "propose_curriculum", in.URLs()); err != nil {
			return nil, nil, err
		}
		if err := w.Propose(in); err != nil {
			return nil, nil, fmt.Errorf("%w\nFix these and call propose_curriculum again", err)
		}
		emit(curriculumProposed, WorkspaceEvent{Workspace: w.ID})
		return textResult("The proposal is shown to the learner. End your turn now. If they approve, the app has writers prepare the first lessons and tasks; if they want changes, you'll get a message."), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "set_toolchain",
		Description: "Set up a programming language the app doesn't ship (it ships go and python), or change one you set up before, so the curriculum and tasks can use it. " +
			"The app builds the sandbox image (this can take several minutes) and runs the example's tests, which must all pass. Only then is the language saved. " +
			"If something fails, fix the toolchain using the report and call set_toolchain again. A language server that doesn't answer isn't a failure: the language is saved without it. " +
			"Changing a language affects its existing tasks, so keep the test command compatible.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in setToolchainInput) (*mcp.CallToolResult, any, error) {
		tc, err := in.Toolchain.Toolchain()
		if err != nil {
			return nil, nil, err
		}
		emit(gateStatus, GateEvent{Workspace: w.ID, Gate: Gate{Kind: "toolchain", Title: tc.Name, Running: true}})
		report, err := a.checkToolchain(ctx, tc, in.Example)
		// The editor's language server is a nice-to-have: without one the
		// editor still highlights syntax, so it doesn't fail the setup.
		note := ""
		if err == nil && report == "" && len(tc.LanguageServer) > 0 {
			files := maps.Clone(in.Example.Solution)
			maps.Copy(files, in.Example.Tests)
			if lspErr := a.probeLanguageServer(ctx, tc, files); lspErr != nil {
				in.Toolchain.LanguageServer = nil
				note = " Its language server didn't work, so it's saved without one and the editor only highlights syntax: " + lspErr.Error() +
					". To add one, fix language_server and call set_toolchain again."
			}
		}
		emit(gateStatus, GateEvent{Workspace: w.ID, Gate: Gate{Kind: "toolchain", Title: tc.Name, Passed: err == nil && report == ""}})
		if err != nil {
			return nil, nil, fmt.Errorf("sandbox problem: %w", err)
		}
		if report != "" {
			return nil, nil, errors.New(report + "\nFix these and call set_toolchain again.")
		}
		if err := w.SaveToolchain(in.Toolchain); err != nil {
			return nil, nil, err
		}
		emit(workspaceChanged, WorkspaceEvent{Workspace: w.ID})
		return textResult(fmt.Sprintf("%s is set up. Use language %q in the curriculum.%s", tc.Name, tc.Language, note)), nil, nil
	})
	return s
}

func textResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}

// addWriterTools adds the tools that publish an objective's lesson and task.
// Only writers get them, so the tutoring and planning chats stay about the
// learner.
func (h *mcpHost) addWriterTools(s *mcp.Server, w *workspace.Workspace, objective string) {
	a := h.app
	mine := func(id string) error {
		if id != objective {
			return fmt.Errorf("you write for the objective %q only, not %q", objective, id)
		}
		return nil
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "create_task",
		Description: "Submit a task for a curriculum objective. The app runs a quality gate in a sandbox: the reference must pass all tests 3 times, " +
			"the starter must compile and fail, and every mutant must compile and fail. Only then is the task shown to the learner. The statement must stand on its own: no feedback on earlier tasks or chat remarks. " +
			"If the gate fails, fix the task using the report and call create_task again.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, spec workspace.TaskSpec) (*mcp.CallToolResult, createTaskOutput, error) {
		if err := mine(spec.ObjectiveID); err != nil {
			return nil, createTaskOutput{}, err
		}
		tc, err := w.ValidateSpec(&spec)
		if err != nil {
			return nil, createTaskOutput{}, err
		}
		a.writerCheck(w.ID, Gate{Objective: spec.ObjectiveID, Title: spec.Title, Running: true})
		rep, err := a.runner.Gate(ctx, runner.GateInput{
			Toolchain: tc, Starter: spec.Starter, Reference: spec.Reference,
			Tests: spec.Tests, HiddenTests: spec.HiddenTests, Mutants: spec.Mutants,
		})
		a.writerCheck(w.ID, Gate{Objective: spec.ObjectiveID, Title: spec.Title, Passed: rep.Passed})
		if err != nil {
			return nil, createTaskOutput{}, fmt.Errorf("sandbox problem (not your fault, tell the learner): %w", err)
		}
		out := createTaskOutput{Gate: &rep, Report: rep.String()}
		if !rep.Passed {
			// An error result that still carries the structured gate report.
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		info, err := w.SaveTask(spec, tc.Language, rep.Names)
		if err != nil {
			return nil, out, err
		}
		out.Published, out.TaskID = true, info.ID
		out.Report += "Task " + info.ID + " is now available to the learner."
		emit(taskPublished, TaskEvent{Workspace: w.ID, Task: info})
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "create_lesson",
		Description: "Publish the lesson for a curriculum objective: the theory the learner reads before that objective's task, ending in a short quiz the app checks. " +
			"Write it before the objective's first task. One lesson per objective. If the lesson is rejected, fix the problems and call create_lesson again.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, spec workspace.LessonSpec) (*mcp.CallToolResult, any, error) {
		if err := mine(spec.ObjectiveID); err != nil {
			return nil, nil, err
		}
		if err := w.ValidateLesson(spec); err != nil {
			return nil, nil, fmt.Errorf("%w\nFix these and call create_lesson again", err)
		}
		if err := a.brokenLinks(ctx, "create_lesson", append(markdownLinks(spec.Body), spec.PrimarySource.URL)); err != nil {
			return nil, nil, err
		}
		info, err := w.SaveLesson(spec)
		if err != nil {
			return nil, nil, err
		}
		emit(lessonPublished, LessonEvent{Workspace: w.ID, Lesson: info})
		return textResult("The lesson " + info.ID + " is available to the learner. Now create the task for this objective, if it doesn't have one."), nil, nil
	})
}

// checkToolchain builds a new language's image and proves it works. It
// returns a report for the tutor when the setup is wrong, or an error when
// the sandbox itself is broken.
func (a *App) checkToolchain(ctx context.Context, tc runner.Toolchain, ex toolchainExample) (string, error) {
	var problems []string
	if len(ex.Solution) == 0 || len(ex.Tests) == 0 {
		problems = append(problems, "the example needs a solution file and a test file")
	}
	for name := range ex.Tests {
		if !tc.IsTestFile(name) {
			problems = append(problems, fmt.Sprintf("example test %s doesn't match test_files", name))
		}
	}
	for name := range ex.Solution {
		if tc.IsTestFile(name) {
			problems = append(problems, fmt.Sprintf("example solution %s matches test_files", name))
		}
	}
	if len(problems) > 0 {
		return "Toolchain check FAILED.\n- " + strings.Join(problems, "\n- "), nil
	}
	if err := a.runner.Prepare(ctx, tc); err != nil {
		// Build errors are the tutor's to fix, not a broken sandbox.
		if a.runner.Check(ctx) != nil {
			return "", err
		}
		return "Toolchain check FAILED: the image didn't build.\n" + err.Error(), nil
	}
	res, err := a.runner.Run(ctx, runner.Job{Toolchain: tc, Solution: ex.Solution, Tests: ex.Tests})
	if err != nil {
		return "", err
	}
	if res.Status != runner.StatusPassed {
		var b strings.Builder
		fmt.Fprintf(&b, "Toolchain check FAILED: the example's tests should all pass, but the run ended with status %q", res.Status)
		if res.Status == runner.StatusError {
			b.WriteString(" (no readable JUnit report at /tmp/junit.xml, or the code didn't build)")
		}
		for _, t := range res.Tests {
			if !t.Passed {
				fmt.Fprintf(&b, "\n- %s failed: %s", t.Name, text.Tail(t.Message, 400))
			}
		}
		fmt.Fprintf(&b, "\nOutput:\n%s", text.Tail(res.Output, 2000))
		return b.String(), nil
	}
	return "", nil
}
