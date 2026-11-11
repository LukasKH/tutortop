package mcp

import (
	"context"

	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// A writer's tool that publishes its objective's task, once it passes the
// quality gate.

var createTaskTool = &mcpsdk.Tool{
	Name: "create_task",
	Description: "Submit a task for a curriculum objective. The app runs a quality gate in a sandbox: the reference must pass every test, " +
		"and the visible tests on their own, and the starter must compile and fail. Only then is the task shown to the learner. The statement must stand on its own: no feedback on earlier tasks or chat remarks. " +
		"If the gate fails, fix the task using the report and call create_task again.",
}

type createTaskOutput struct {
	Published bool                `json:"published"`
	TaskID    string              `json:"taskId,omitempty"`
	Gate      *sandbox.GateReport `json:"gate,omitempty"`
	Report    string              `json:"report"`
}

// createTask publishes a task only once it passes the quality gate.
func (t *threadTools) createTask(ctx context.Context, _ *mcpsdk.CallToolRequest, spec workspace.TaskSpec) (*mcpsdk.CallToolResult, createTaskOutput, error) {
	if err := t.ownObjective(spec.ObjectiveID); err != nil {
		return nil, createTaskOutput{}, err
	}
	spec, tc, err := t.w.ValidateSpec(spec)
	if err != nil {
		return nil, createTaskOutput{}, err
	}
	rep, err := t.qualityGate(ctx, spec, tc)
	if err != nil {
		return nil, createTaskOutput{}, sandboxProblem(err)
	}
	out := createTaskOutput{Gate: &rep, Report: rep.String()}
	if !rep.Passed {
		// Unlike a returned error, this error result keeps the structured
		// gate report, so the agent sees which check failed.
		return &mcpsdk.CallToolResult{IsError: true}, out, nil
	}
	info, err := t.w.SaveTask(spec, tc.Language, rep.Names)
	if err != nil {
		return nil, out, err
	}
	out.Published, out.TaskID = true, info.ID
	out.Report += "Task " + info.ID + " is now available to the learner."
	events.Emit(events.TaskPublished, events.Task{Workspace: t.w.ID, Task: info})
	return nil, out, nil
}

// qualityGate runs a task's quality gate, showing it on the objective's page
// while it runs and after.
func (t *threadTools) qualityGate(ctx context.Context, spec workspace.TaskSpec, tc sandbox.Toolchain) (sandbox.GateReport, error) {
	status := events.SandboxCheck{Kind: events.CheckTask, Thread: t.thread.String(), Title: spec.Title, Running: true}
	t.checks.Show(t.w.ID, status)
	rep, err := t.runner.Gate(ctx, sandbox.GateInput{
		Toolchain:   tc,
		Starter:     spec.Starter,
		Reference:   spec.Reference,
		Tests:       spec.Tests,
		HiddenTests: spec.HiddenTests,
	})
	status.Running, status.Passed = false, rep.Passed
	t.checks.Show(t.w.ID, status)
	return rep, err
}
