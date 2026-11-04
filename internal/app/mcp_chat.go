package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/text"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tools of the chats: the plan, a task's and a lesson's.

var askUserTool = &mcp.Tool{
	Name:        "ask_user",
	Description: "Show the learner 2-4 focused questions as a form in the app. End your turn after calling this; the answers arrive as the next user message.",
}

type askUserInput struct {
	Intro     string               `json:"intro,omitempty" jsonschema:"one sentence shown above the questions"`
	Questions []workspace.Question `json:"questions" jsonschema:"2-4 focused questions"`
}

// askUser shows a form in this thread. The answers come back to this thread,
// so the form remembers which one asked.
func (t *threadTools) askUser(_ context.Context, _ *mcp.CallToolRequest, in askUserInput) (*mcp.CallToolResult, any, error) {
	if len(in.Questions) < 2 || len(in.Questions) > 4 {
		return nil, nil, errors.New("ask 2-4 questions")
	}
	q := workspace.Questions{Thread: t.thread.String(), Intro: in.Intro, Questions: in.Questions}
	if err := t.w.SetQuestions(&q); err != nil {
		return nil, nil, err
	}
	emit(questionsAsked, QuestionsEvent{Workspace: t.w.ID, Questions: q})
	return textResult("The learner can see the questions now. End your turn; the answers will arrive as the next message."), nil, nil
}

var setIconTool = &mcp.Tool{
	Name: "set_icon",
	Description: "Pick the picture the app shows for this workspace on its card, in the curriculum and on lessons. " +
		"Choose the language or tool the workspace centres on, else the glyph closest to the topic. One of: " + strings.Join(workspace.Icons, ", ") + ".",
}

type setIconInput struct {
	Icon string `json:"icon"`
}

func (t *threadTools) setIcon(_ context.Context, _ *mcp.CallToolRequest, in setIconInput) (*mcp.CallToolResult, any, error) {
	if err := t.w.SetIcon(in.Icon); err != nil {
		return nil, nil, err
	}
	emit(workspaceChanged, WorkspaceEvent{Workspace: t.w.ID})
	return textResult("The workspace shows the " + in.Icon + " icon now."), nil, nil
}

var proposeCurriculumTool = &mcp.Tool{
	Name:        "propose_curriculum",
	Description: "Show a curriculum (new or revised) to the learner for approval. Every objective must be verifiable with unit tests. Keep ids of unchanged objectives so progress carries over. End your turn after calling this.",
}

// proposeCurriculum saves a proposal for the learner to approve. Approving
// it is the learner's call, in the app, not the agent's.
func (t *threadTools) proposeCurriculum(ctx context.Context, _ *mcp.CallToolRequest, in workspace.Curriculum) (*mcp.CallToolResult, any, error) {
	if err := t.app.brokenLinks(ctx, proposeCurriculumTool, in.URLs()); err != nil {
		return nil, nil, err
	}
	if err := t.w.Propose(in); err != nil {
		return nil, nil, fixAndRetry(proposeCurriculumTool, err)
	}
	emit(curriculumProposed, WorkspaceEvent{Workspace: t.w.ID})
	return textResult("The proposal is shown to the learner. End your turn now. If they approve, the app has writers prepare the first lessons and tasks; if they want changes, you'll get a message."), nil, nil
}

var setToolchainTool = &mcp.Tool{
	Name: "set_toolchain",
	Description: "Set up a programming language the app doesn't ship (it ships " + strings.Join(runner.BuiltinLanguages(), " and ") + "), or change one you set up before, so the curriculum and tasks can use it. " +
		"The app builds the sandbox image (this can take several minutes) and runs the example's tests, which must all pass. Only then is the language saved. " +
		"If something fails, fix the toolchain using the report and call set_toolchain again. A language server that doesn't answer isn't a failure: the language is saved without it. " +
		"Changing a language affects its existing tasks, so keep the test command compatible.",
}

type setToolchainInput struct {
	Toolchain runner.ToolchainSpec `json:"toolchain"`
	Example   toolchainExample     `json:"example" jsonschema:"a tiny solution and test file that must pass, to prove the setup works end to end"`
}

type toolchainExample struct {
	Solution runner.Files `json:"solution" jsonschema:"filename -> content"`
	Tests    runner.Files `json:"tests" jsonschema:"filename -> content; at least one file matching test_files"`
}

// setToolchain saves a language only once its example passes in the sandbox.
// The learner watches the check like a task's quality gate.
func (t *threadTools) setToolchain(ctx context.Context, _ *mcp.CallToolRequest, in setToolchainInput) (*mcp.CallToolResult, any, error) {
	tc, err := in.Toolchain.Toolchain()
	if err != nil {
		return nil, nil, err
	}
	check := SandboxCheck{Kind: CheckToolchain, Thread: t.thread.String(), Title: tc.Name, Running: true}
	emit(checkStatus, CheckEvent{Workspace: t.w.ID, Check: check})
	report, err := t.app.checkToolchain(ctx, tc, in.Example)
	var lspErr error
	if err == nil && report == "" && len(tc.LanguageServer) > 0 {
		files := maps.Clone(in.Example.Solution)
		maps.Copy(files, in.Example.Tests)
		lspErr = t.app.probeLanguageServer(ctx, tc, files)
	}
	check.Running, check.Passed = false, err == nil && report == ""
	emit(checkStatus, CheckEvent{Workspace: t.w.ID, Check: check})
	if err != nil {
		return nil, nil, sandboxProblem(err)
	}
	if report != "" {
		return nil, nil, fixAndRetry(setToolchainTool, errors.New(report))
	}

	// The editor's language server is a nice-to-have: without one the
	// editor still highlights syntax, so it doesn't fail the setup.
	note := ""
	if lspErr != nil {
		in.Toolchain.LanguageServer = nil
		note = " Its language server didn't work, so it's saved without one and the editor only highlights syntax: " + lspErr.Error() +
			". To add one, fix language_server and call set_toolchain again."
	}
	if err := t.w.SaveToolchain(in.Toolchain); err != nil {
		return nil, nil, err
	}
	emit(workspaceChanged, WorkspaceEvent{Workspace: t.w.ID})
	return textResult(fmt.Sprintf("%s is set up. Use language %q in the curriculum.%s", tc.Name, tc.Language, note)), nil, nil
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
