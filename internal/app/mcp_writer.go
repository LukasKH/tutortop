package app

import (
	"context"
	"fmt"

	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tools of a writer, which publish its objective's lesson and task.

var createTaskTool = &mcp.Tool{
	Name: "create_task",
	Description: "Submit a task for a curriculum objective. The app runs a quality gate in a sandbox: the reference must pass all tests 3 times, " +
		"the starter must compile and fail, and every mutant must compile and fail. Only then is the task shown to the learner. The statement must stand on its own: no feedback on earlier tasks or chat remarks. " +
		"If the gate fails, fix the task using the report and call create_task again.",
}

type createTaskOutput struct {
	Published bool               `json:"published"`
	TaskID    string             `json:"taskId,omitempty"`
	Gate      *runner.GateReport `json:"gate,omitempty"`
	Report    string             `json:"report"`
}

// createTask publishes a task only once it passes the quality gate.
func (t *threadTools) createTask(ctx context.Context, _ *mcp.CallToolRequest, spec workspace.TaskSpec) (*mcp.CallToolResult, createTaskOutput, error) {
	if err := t.ownObjective(spec.ObjectiveID); err != nil {
		return nil, createTaskOutput{}, err
	}
	tc, err := t.w.ValidateSpec(&spec)
	if err != nil {
		return nil, createTaskOutput{}, err
	}
	rep, err := t.gate(ctx, spec, tc)
	if err != nil {
		return nil, createTaskOutput{}, sandboxProblem(err)
	}
	out := createTaskOutput{Gate: &rep, Report: rep.String()}
	if !rep.Passed {
		// Unlike a returned error, this error result keeps the structured
		// gate report, so the agent sees which check failed.
		return &mcp.CallToolResult{IsError: true}, out, nil
	}
	info, err := t.w.SaveTask(spec, tc.Language, rep.Names)
	if err != nil {
		return nil, out, err
	}
	out.Published, out.TaskID = true, info.ID
	out.Report += "Task " + info.ID + " is now available to the learner."
	emit(taskPublished, TaskEvent{Workspace: t.w.ID, Task: info})
	return nil, out, nil
}

// gate runs a task's quality gate, showing it on the objective's page while
// it runs and after.
func (t *threadTools) gate(ctx context.Context, spec workspace.TaskSpec, tc runner.Toolchain) (runner.GateReport, error) {
	status := Gate{Kind: GateTask, Objective: spec.ObjectiveID, Title: spec.Title, Running: true}
	t.app.writerCheck(t.w.ID, status)
	rep, err := t.app.runner.Gate(ctx, runner.GateInput{
		Toolchain: tc, Starter: spec.Starter, Reference: spec.Reference,
		Tests: spec.Tests, HiddenTests: spec.HiddenTests, Mutants: spec.Mutants,
	})
	status.Running, status.Passed = false, rep.Passed
	t.app.writerCheck(t.w.ID, status)
	return rep, err
}

var createLessonTool = &mcp.Tool{
	Name: "create_lesson",
	Description: "Publish the lesson for a curriculum objective: the theory the learner reads before that objective's task, ending in a short quiz the app checks. " +
		"Write it before the objective's first task. One lesson per objective. If the lesson is rejected, fix the problems and call create_lesson again.",
}

func (t *threadTools) createLesson(ctx context.Context, _ *mcp.CallToolRequest, spec workspace.LessonSpec) (*mcp.CallToolResult, any, error) {
	if err := t.ownObjective(spec.ObjectiveID); err != nil {
		return nil, nil, err
	}
	if err := t.w.ValidateLesson(spec); err != nil {
		return nil, nil, fixAndRetry(createLessonTool, err)
	}
	if err := t.app.brokenLinks(ctx, createLessonTool, append(markdownLinks(spec.Body), spec.PrimarySource.URL)); err != nil {
		return nil, nil, err
	}
	info, err := t.w.SaveLesson(spec)
	if err != nil {
		return nil, nil, err
	}
	emit(lessonPublished, LessonEvent{Workspace: t.w.ID, Lesson: info})
	return textResult("The lesson " + info.ID + " is available to the learner. Now create the task for this objective, if it doesn't have one."), nil, nil
}

// ownObjective refuses work on another objective, which has its own writer.
func (t *threadTools) ownObjective(id string) error {
	if id != t.thread.ID {
		return fmt.Errorf("you write for the objective %q only, not %q", t.thread.ID, id)
	}
	return nil
}
