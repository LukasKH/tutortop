package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LukasKH/tutortop/internal/agent/threads"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Feedback is what the learner says about a lesson or exercise they just
// finished: any of the chips, and optionally their own words.
type Feedback struct {
	Chips []FeedbackChip `json:"chips"`
	Text  string         `json:"text"`
}

// FeedbackChip is one tap of "How was this?".
type FeedbackChip string

const (
	FeedbackTooTechnical    FeedbackChip = "tooTechnical"
	FeedbackClear           FeedbackChip = "clear"
	FeedbackTooLong         FeedbackChip = "tooLong"
	FeedbackRightLength     FeedbackChip = "rightLength"
	FeedbackTooEasy         FeedbackChip = "tooEasy"
	FeedbackTooHard         FeedbackChip = "tooHard"
	FeedbackMoreComparisons FeedbackChip = "moreComparisons"
)

// sendFeedback tells the planning tutor what the learner thought of what
// (`the exercise “…”`), so it updates their preferences. Only lessons and
// exercises written afterwards follow them: rewriting ones already written
// ahead would redo work for a single opinion. It fails if the tutor can't take
// the message, so the learner can send it again.
func sendFeedback(agents *threads.Agents, w *workspace.Workspace, what string, fb Feedback) error {
	said, err := fb.sentences(w.PriorKnowledge())
	if err != nil {
		return err
	}
	shown := "Your feedback on " + what + ": " + said
	return agents.TrySend(w, workspace.PlanThread, threads.Message{
		Role:  workspace.RoleNotice,
		Shown: shown,
		Agent: fmt.Sprintf("[app] My feedback on %s: %s "+
			"Turn it into my preferences: update NOTES.md with how I want lessons and exercises written (style, length, analogies), "+
			"and update task_defaults if I want tasks of another size. Lessons and tasks written from now on follow them, so don't rewrite ones already written, "+
			"and don't write lessons or tasks yourself. Reply in one sentence at most.", what, said),
	})
}

// sentences is the feedback in the learner's words, one sentence per chip,
// then their own text. known is what the learner already knows, for
// comparisons; "" if the mission doesn't say.
func (fb Feedback) sentences(known string) (string, error) {
	var out []string
	for _, c := range fb.Chips {
		s, err := c.sentence(known)
		if err != nil {
			return "", err
		}
		out = append(out, s)
	}
	if text := strings.TrimSpace(fb.Text); text != "" {
		out = append(out, "“"+text+"”")
	}
	if len(out) == 0 {
		return "", errors.New("pick a chip or write something first")
	}
	return strings.Join(out, " "), nil
}

func (c FeedbackChip) sentence(known string) (string, error) {
	switch c {
	case FeedbackTooTechnical:
		return "It was too technical.", nil
	case FeedbackClear:
		return "It was clear.", nil
	case FeedbackTooLong:
		return "It was too long.", nil
	case FeedbackRightLength:
		return "It was the right length.", nil
	case FeedbackTooEasy:
		return "It was too easy.", nil
	case FeedbackTooHard:
		return "It was too hard.", nil
	case FeedbackMoreComparisons:
		if known == "" {
			return "I'd like more comparisons to what I already know.", nil
		}
		return "I'd like more comparisons to " + known + ".", nil
	}
	return "", fmt.Errorf("unknown feedback %q", c)
}
