package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/workspace"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The chats' tools for talking with the learner and shaping the plan: ask
// them questions, pick the workspace's icon, propose a curriculum. The chats
// are the plan, a task's and a lesson's.

var askUserTool = &mcpsdk.Tool{
	Name:        "ask_user",
	Description: "Show the learner 2-4 focused questions as a form in the app. End your turn after calling this; the answers arrive as the next user message.",
}

type askUserInput struct {
	Intro     string               `json:"intro,omitempty" jsonschema:"one sentence shown above the questions"`
	Questions []workspace.Question `json:"questions" jsonschema:"2-4 focused questions"`
}

// askUser shows a form in this thread. The answers come back to this thread,
// so the form remembers which one asked.
func (t *threadTools) askUser(_ context.Context, _ *mcpsdk.CallToolRequest, in askUserInput) (*mcpsdk.CallToolResult, any, error) {
	if len(in.Questions) < 2 || len(in.Questions) > 4 {
		return nil, nil, errors.New("ask 2-4 questions")
	}
	q := workspace.Questions{Thread: t.thread.String(), Intro: in.Intro, Questions: in.Questions}
	if err := t.w.SetQuestions(&q); err != nil {
		return nil, nil, err
	}
	events.Emit(events.QuestionsAsked, events.Questions{Workspace: t.w.ID, Questions: q})
	return textResult("The learner can see the questions now. End your turn; the answers will arrive as the next message."), nil, nil
}

var setIconTool = &mcpsdk.Tool{
	Name: "set_icon",
	Description: "Pick the picture the app shows for this workspace on its card, in the curriculum and on lessons. " +
		"Choose the language or tool the workspace centres on, else the glyph closest to the topic. One of: " + strings.Join(workspace.Icons(), ", ") + ".",
}

type setIconInput struct {
	Icon string `json:"icon"`
}

func (t *threadTools) setIcon(_ context.Context, _ *mcpsdk.CallToolRequest, in setIconInput) (*mcpsdk.CallToolResult, any, error) {
	if err := t.w.SetIcon(in.Icon); err != nil {
		return nil, nil, err
	}
	events.Emit(events.WorkspaceChanged, events.Workspace{Workspace: t.w.ID})
	return textResult("The workspace shows the " + in.Icon + " icon now."), nil, nil
}

var proposeCurriculumTool = &mcpsdk.Tool{
	Name:        "propose_curriculum",
	Description: "Show a curriculum (new or revised) to the learner for approval. Every objective must be verifiable with unit tests. Keep ids of unchanged objectives so progress carries over. End your turn after calling this.",
}

// proposeCurriculum saves a proposal for the learner to approve. Approving
// it is the learner's call, in the app, not the agent's.
func (t *threadTools) proposeCurriculum(ctx context.Context, _ *mcpsdk.CallToolRequest, in workspace.Curriculum) (*mcpsdk.CallToolResult, any, error) {
	if err := t.links.check(ctx, proposeCurriculumTool, in.URLs()); err != nil {
		return nil, nil, err
	}
	if err := t.w.Propose(in); err != nil {
		return nil, nil, fixAndRetry(proposeCurriculumTool, err)
	}
	events.Emit(events.CurriculumProposed, events.Workspace{Workspace: t.w.ID})
	return textResult("The proposal is shown to the learner. End your turn now. If they approve, the app has writers prepare the first lessons and tasks; if they want changes, you'll get a message."), nil, nil
}
