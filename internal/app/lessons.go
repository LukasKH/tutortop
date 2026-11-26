package app

import (
	"fmt"

	"github.com/LukasKH/tutortop/internal/agent/threads"
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// LessonService serves the lessons: a lesson and its quiz.
type LessonService struct {
	store  *workspace.Store
	agents *threads.Agents // tells the plan's tutor the learner's feedback
}

func newLessonService(store *workspace.Store, agents *threads.Agents) *LessonService {
	return &LessonService{store: store, agents: agents}
}

func (s *LessonService) Lesson(wsID, lessonID string) (workspace.LessonDetail, error) {
	w, err := s.store.Open(wsID)
	if err != nil {
		return workspace.LessonDetail{}, err
	}
	return w.Lesson(lessonID)
}

// AnswerQuiz checks one quiz answer and returns the feedback. Only the first
// answer to a question is recorded.
func (s *LessonService) AnswerQuiz(wsID, lessonID string, question, choice int) (workspace.QuizResult, error) {
	w, err := s.store.Open(wsID)
	if err != nil {
		return workspace.QuizResult{}, err
	}
	r, err := w.AnswerQuiz(lessonID, question, choice)
	if err != nil {
		return r, err
	}
	events.Emit(events.WorkspaceChanged, events.Workspace{Workspace: wsID})
	return r, nil
}

// SendFeedback tells the planning tutor what the learner thought of a
// lesson, so later lessons and exercises fit them better.
func (s *LessonService) SendFeedback(wsID, lessonID string, fb Feedback) error {
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	l, err := w.Lesson(lessonID)
	if err != nil {
		return err
	}
	return sendFeedback(s.agents, w, fmt.Sprintf("the lesson “%s”", l.Title), fb)
}
