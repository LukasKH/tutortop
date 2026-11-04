package mcp

import (
	"context"

	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/workspace"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// A writer's tool that publishes its objective's lesson.

var createLessonTool = &mcpsdk.Tool{
	Name: "create_lesson",
	Description: "Publish the lesson for a curriculum objective: the theory the learner reads before that objective's task, ending in a short quiz the app checks. " +
		"Write it before the objective's first task. One lesson per objective. If the lesson is rejected, fix the problems and call create_lesson again.",
}

func (t *threadTools) createLesson(ctx context.Context, _ *mcpsdk.CallToolRequest, spec workspace.LessonSpec) (*mcpsdk.CallToolResult, any, error) {
	if err := t.ownObjective(spec.ObjectiveID); err != nil {
		return nil, nil, err
	}
	if err := t.w.ValidateLesson(spec); err != nil {
		return nil, nil, fixAndRetry(createLessonTool, err)
	}
	if err := t.links.check(ctx, createLessonTool, append(markdownLinks(spec.Body), spec.PrimarySource.URL)); err != nil {
		return nil, nil, err
	}
	info, err := t.w.SaveLesson(spec)
	if err != nil {
		return nil, nil, err
	}
	events.Emit(events.LessonPublished, events.Lesson{Workspace: t.w.ID, Lesson: info})
	return textResult("The lesson " + info.ID + " is available to the learner. Now create the task for this objective, if it doesn't have one."), nil, nil
}
