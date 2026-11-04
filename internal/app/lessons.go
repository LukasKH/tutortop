package app

import "github.com/LukasKH/tutortop/internal/workspace"

func (a *App) Lesson(wsID, lessonID string) (workspace.LessonDetail, error) {
	w, err := a.store.Open(wsID)
	if err != nil {
		return workspace.LessonDetail{}, err
	}
	return w.Lesson(lessonID)
}

// AnswerQuiz checks one quiz answer and returns the feedback. Only the first
// answer to a question is recorded.
func (a *App) AnswerQuiz(wsID, lessonID string, question, choice int) (workspace.QuizResult, error) {
	w, err := a.store.Open(wsID)
	if err != nil {
		return workspace.QuizResult{}, err
	}
	r, err := w.AnswerQuiz(lessonID, question, choice)
	if err == nil {
		emit(workspaceChanged, WorkspaceEvent{Workspace: wsID})
	}
	return r, err
}
