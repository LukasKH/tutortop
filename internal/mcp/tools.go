package mcp

import (
	"context"
	"slices"

	"github.com/LukasKH/tutortop/internal/workspace"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolNames lists the tools a kind of thread gets, in the tools' order. The
// agent's instructions list them too.
func ToolNames(kind workspace.ThreadKind) []string {
	var names []string
	for _, tool := range toolsFor(kind) {
		names = append(names, tool.name)
	}
	return names
}

// ChatLine is the line calls to one of our tools show as in the chat, and
// whether they show at all.
func ChatLine(tool string) (line string, shown bool) {
	for _, t := range tools {
		if t.name == tool {
			return t.line, t.line != hidden
		}
	}
	return hidden, false
}

// tools lists every tool, the line its calls show as in the chat, and the
// kinds of thread that get it. Chats (the plan, a task's, a lesson's) talk
// with the learner and shape the plan. Only an objective's writer publishes
// its lesson and task, so the chats stay about the learner.
//
// Most calls are hidden: reading the state is noise, and the others show in
// their own UI (the questions form, the icon, the proposal screen). Writers
// have no chat; the objective's page shows their work.
var tools = []threadTool{
	toolFor(getStateTool, (*threadTools).getState, hidden, workspace.PlanKind, workspace.TaskKind, workspace.LessonKind, workspace.WriterKind),
	toolFor(askUserTool, (*threadTools).askUser, hidden, workspace.PlanKind, workspace.TaskKind, workspace.LessonKind),
	toolFor(setIconTool, (*threadTools).setIcon, hidden, workspace.PlanKind, workspace.TaskKind, workspace.LessonKind),
	toolFor(proposeCurriculumTool, (*threadTools).proposeCurriculum, hidden, workspace.PlanKind, workspace.TaskKind, workspace.LessonKind),
	// The title Claude Code and Codex give the call, as it has always shown.
	toolFor(setToolchainTool, (*threadTools).setToolchain, "tutortop · set_toolchain", workspace.PlanKind, workspace.TaskKind, workspace.LessonKind),
	toolFor(createTaskTool, (*threadTools).createTask, hidden, workspace.WriterKind),
	toolFor(createLessonTool, (*threadTools).createLesson, hidden, workspace.WriterKind),
}

// threadTool is one tool, the line its calls show as in the chat, and the
// kinds of thread that get it.
type threadTool struct {
	name  string
	line  string // in the chat; hidden keeps its calls out
	kinds []workspace.ThreadKind
	add   func(*mcpsdk.Server, *threadTools) // adds the tool, handled by one thread's threadTools
}

// hidden is the chat line of a tool whose calls don't show in the chat.
const hidden = ""

func toolFor[In, Out any](tool *mcpsdk.Tool, handle func(*threadTools, context.Context, *mcpsdk.CallToolRequest, In) (*mcpsdk.CallToolResult, Out, error), line string, kinds ...workspace.ThreadKind) threadTool {
	return threadTool{name: tool.Name, line: line, kinds: kinds, add: func(s *mcpsdk.Server, t *threadTools) {
		mcpsdk.AddTool(s, tool, func(ctx context.Context, req *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, Out, error) {
			return handle(t, ctx, req, in)
		})
	}}
}

// toolsFor returns the tools a kind of thread gets, in tools' order.
func toolsFor(kind workspace.ThreadKind) []threadTool {
	var forKind []threadTool
	for _, tool := range tools {
		if slices.Contains(tool.kinds, kind) {
			forKind = append(forKind, tool)
		}
	}
	return forKind
}
