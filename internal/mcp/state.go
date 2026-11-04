package mcp

import (
	"context"

	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tool every thread gets: reading the workspace's state.

var getStateTool = &mcpsdk.Tool{
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
	Toolchains []sandbox.ToolchainSpec  `json:"toolchains" jsonschema:"languages set up with set_toolchain, besides the built-in go and python"`
}

func (t *threadTools) getState(context.Context, *mcpsdk.CallToolRequest, struct{}) (*mcpsdk.CallToolResult, stateOutput, error) {
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
