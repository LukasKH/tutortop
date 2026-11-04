package mcp

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/text"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The chats' tool for setting up a language the app doesn't ship.

var setToolchainTool = &mcpsdk.Tool{
	Name: "set_toolchain",
	Description: "Set up a programming language the app doesn't ship (it ships " + text.List(sandbox.BuiltinLanguages()) + "), or change one you set up before, so the curriculum and tasks can use it. " +
		"The app builds the sandbox image (this can take several minutes) and runs the example's tests, which must all pass. Only then is the language saved. " +
		"If something fails, fix the toolchain using the report and call set_toolchain again. A language server that doesn't answer isn't a failure: the language is saved without it. " +
		"Changing a language affects its existing tasks, so keep the test command compatible.",
}

type setToolchainInput struct {
	Toolchain sandbox.ToolchainSpec `json:"toolchain"`
	Example   toolchainExample      `json:"example" jsonschema:"a tiny solution and test file that must pass, to prove the setup works end to end"`
}

type toolchainExample struct {
	Solution sandbox.Files `json:"solution" jsonschema:"filename -> content"`
	Tests    sandbox.Files `json:"tests" jsonschema:"filename -> content; at least one file matching test_files"`
}

// setToolchain saves a language only once its example passes in the sandbox.
// The learner watches the check like a task's quality gate.
func (t *threadTools) setToolchain(ctx context.Context, _ *mcpsdk.CallToolRequest, in setToolchainInput) (*mcpsdk.CallToolResult, any, error) {
	tc, err := in.Toolchain.Toolchain()
	if err != nil {
		return nil, nil, err
	}
	if _, ok := sandbox.Builtin(tc.Language); ok {
		return nil, nil, fmt.Errorf("%s is built in; use it directly", tc.Language)
	}
	check := events.SandboxCheck{Kind: events.CheckToolchain, Thread: t.thread.String(), Title: tc.Name, Running: true}
	t.checks.Show(t.w.ID, check)
	report, err := t.checkToolchain(ctx, tc, in.Example)
	var lspErr error
	if err == nil && report == "" && len(tc.LanguageServer) > 0 {
		files := maps.Clone(in.Example.Solution)
		maps.Copy(files, in.Example.Tests)
		lspErr = t.runner.ProbeLanguageServer(ctx, tc, files)
	}
	check.Running, check.Passed = false, err == nil && report == ""
	t.checks.Show(t.w.ID, check)
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
	events.Emit(events.WorkspaceChanged, events.Workspace{Workspace: t.w.ID})
	return textResult(fmt.Sprintf("%s is set up. Use language %q in the curriculum.%s", tc.Name, tc.Language, note)), nil, nil
}

// checkToolchain builds a new language's image and proves it works. It
// returns a report for the tutor when the setup is wrong, or an error when
// the sandbox itself is broken.
func (t *threadTools) checkToolchain(ctx context.Context, tc sandbox.Toolchain, ex toolchainExample) (string, error) {
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
	if err := t.runner.Prepare(ctx, tc); err != nil {
		// Build errors are the tutor's to fix, not a broken sandbox.
		if t.runner.Available(ctx) != nil {
			return "", err
		}
		return "Toolchain check FAILED: the image didn't build.\n" + err.Error(), nil
	}
	res, err := t.runner.Run(ctx, sandbox.Job{Toolchain: tc, Solution: ex.Solution, Tests: ex.Tests})
	if err != nil {
		return "", err
	}
	if res.Status != sandbox.StatusPassed {
		var b strings.Builder
		fmt.Fprintf(&b, "Toolchain check FAILED: the example's tests should all pass, but the run ended with status %q", res.Status)
		if res.Status == sandbox.StatusError {
			b.WriteString(" (no readable JUnit report at /tmp/junit.xml, or the code didn't build)")
		}
		for _, tr := range res.Tests {
			if !tr.Passed {
				fmt.Fprintf(&b, "\n- %s failed: %s", tr.Name, text.Tail(tr.Message, 400))
			}
		}
		fmt.Fprintf(&b, "\nOutput:\n%s", text.Tail(res.Output, 2000))
		return b.String(), nil
	}
	return "", nil
}
