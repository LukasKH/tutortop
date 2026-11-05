package workspace

import (
	"time"

	"github.com/LukasKH/tutortop/internal/sandbox"
)

// AttemptKind is what the learner did.
type AttemptKind string

const (
	AttemptRun    AttemptKind = "run"    // checked the visible tests
	AttemptSubmit AttemptKind = "submit" // checked every test; only a passed submit is progress
	AttemptHint   AttemptKind = "hint"
	AttemptQuiz   AttemptKind = "quiz" // answered a lesson's quiz question
)

// Attempt is one learner action on a task or lesson, written by the app only.
type Attempt struct {
	TaskID    string         `json:"taskId,omitempty"`
	Kind      AttemptKind    `json:"kind"`
	LessonID  string         `json:"lessonId,omitempty"`
	Question  int            `json:"question,omitempty"` // quiz: question index
	Choice    int            `json:"choice,omitempty"`   // quiz: option picked
	Status    sandbox.Status `json:"status,omitempty"`   // run and submit: how the check ended
	Passed    int            `json:"passed,omitempty"`   // tests passed; quiz: 1 if correct
	Total     int            `json:"total,omitempty"`
	HintLevel int            `json:"hintLevel,omitempty"`
	At        time.Time      `json:"at"`
}

func (w *Workspace) RecordAttempt(a Attempt) error {
	if a.At.IsZero() {
		a.At = time.Now()
	}
	return appendJSONL(w.path(".tutortop", "attempts.jsonl"), a)
}

func (w *Workspace) Attempts() ([]Attempt, error) {
	return readJSONL[Attempt](w.path(".tutortop", "attempts.jsonl"))
}

// passedTask reports whether the attempt passed a task: a submission that
// passed every test, the only thing that counts as progress.
func (a Attempt) passedTask() bool {
	return a.Kind == AttemptSubmit && a.Status == sandbox.StatusPassed
}

// history is what the attempt log says about each task and lesson.
type history struct {
	passed map[string]bool               // task id -> passed
	hints  map[string]HintUse            // task id -> hints asked for
	quiz   map[string]map[int]QuizResult // lesson id -> first answer per question
}

func (w *Workspace) history() history {
	h := history{
		passed: map[string]bool{},
		hints:  map[string]HintUse{},
		quiz:   map[string]map[int]QuizResult{},
	}
	attempts, _ := w.Attempts()
	for _, a := range attempts {
		switch a.Kind {
		case AttemptSubmit:
			if a.passedTask() {
				h.passed[a.TaskID] = true
			}
		case AttemptHint:
			u := h.hints[a.TaskID]
			h.hints[a.TaskID] = HintUse{Count: u.Count + 1, Highest: max(u.Highest, a.HintLevel)}
		case AttemptQuiz:
			if h.quiz[a.LessonID] == nil {
				h.quiz[a.LessonID] = map[int]QuizResult{}
			}
			if _, seen := h.quiz[a.LessonID][a.Question]; !seen {
				h.quiz[a.LessonID][a.Question] = QuizResult{Choice: a.Choice, Correct: a.Passed == 1}
			}
		case AttemptRun:
			// A run isn't progress.
		}
	}
	return h
}
