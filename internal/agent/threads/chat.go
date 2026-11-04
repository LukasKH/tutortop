package threads

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ChatService serves the chats: what the learner sends to a thread's agent,
// which answers in the thread's chat as chat:message events. It owns the
// thread agents' lifetime: it ends them all as the app quits.
//
// Agents.Send posts a message in the thread's chat and queues it for the
// thread's agent (agents.go); a transcript (transcript.go) streams the reply
// into the chat as it arrives.
type ChatService struct {
	store  *workspace.Store
	agents *Agents
}

var _ application.ServiceShutdown = (*ChatService)(nil)

// NewChatService serves the chats of the workspaces in store, answered by
// the thread agents.
func NewChatService(store *workspace.Store, agents *Agents) *ChatService {
	return &ChatService{store: store, agents: agents}
}

// ServiceShutdown is called by Wails on exit and ends every thread's agent,
// writers included.
func (s *ChatService) ServiceShutdown() error {
	s.agents.endAll()
	return nil
}

// Chat returns one thread's chat.
func (s *ChatService) Chat(wsID, thread string) ([]workspace.ChatItem, error) {
	w, err := s.store.Open(wsID)
	if err != nil {
		return nil, err
	}
	return w.Chat(thread)
}

// SendMessage sends the learner's chat message to a thread, by its name
// ("plan", a task id, ...). In a task's or lesson's thread the app adds a
// [context] block about it. It returns once the message is queued; the
// reply, or why the agent couldn't answer, arrives in the chat. An error
// means the message wasn't sent at all.
func (s *ChatService) SendMessage(wsID, text, thread string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	t, err := workspace.ParseThread(thread)
	if err != nil {
		return err
	}
	s.agents.Send(w, t, Message{Role: workspace.RoleUser, Shown: text, Agent: contextFor(w, t) + text})
	return nil
}

// AnswerQuestions sends the learner's answers to the thread that asked.
func (s *ChatService) AnswerQuestions(wsID string, answers map[string]string) error {
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	t := workspace.PlanThread
	var b strings.Builder
	b.WriteString("My answers:\n")
	if q := w.PendingQuestions(); q != nil {
		if t, err = workspace.ParseThread(q.Thread); err != nil {
			return err
		}
		for _, qu := range q.Questions {
			fmt.Fprintf(&b, "- %s\n  → %s\n", qu.Question, answers[qu.ID])
		}
	} else {
		for _, k := range slices.Sorted(maps.Keys(answers)) {
			fmt.Fprintf(&b, "- %s: %s\n", k, answers[k])
		}
	}
	if err := w.SetQuestions(nil); err != nil {
		return err
	}
	s.agents.Send(w, t, Message{Role: workspace.RoleUser, Shown: b.String(), Agent: b.String()})
	return nil
}

// DismissQuestions drops the pending form without answering.
func (s *ChatService) DismissQuestions(wsID string) error {
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	return w.SetQuestions(nil)
}

// RequestHint records the hint (it counts against mastery) and asks the
// task's tutor for it.
func (s *ChatService) RequestHint(wsID, taskID string, level int) error {
	if level < 1 || level > 4 {
		return errors.New("hint level must be 1-4")
	}
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	_ = w.RecordAttempt(workspace.Attempt{TaskID: taskID, Kind: workspace.AttemptHint, HintLevel: level})
	names := []string{"", "a nudge", "a pointer", "a snippet", "the solution walkthrough"}
	msg := fmt.Sprintf("Hint level %d (%s), please.", level, names[level])
	t := workspace.TaskThread(taskID)
	s.agents.Send(w, t, Message{Role: workspace.RoleUser, Shown: msg, Agent: contextFor(w, t) + msg})
	return nil
}

// StopAgent interrupts the current turn in a thread. Its agent posts a
// notice once the cut-off reply is saved.
func (s *ChatService) StopAgent(wsID, thread string) { s.agents.stop(wsID, thread) }
