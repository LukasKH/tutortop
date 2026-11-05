package app

import (
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// LessonService serves the lessons: a lesson and its quiz.
type LessonService struct {
	store *workspace.Store
}

func newLessonService(store *workspace.Store) *LessonService {
	return &LessonService{store: store}
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
