package workspace

import (
	"slices"
	"time"

	"github.com/LukasKH/tutortop/internal/runner"
)

// Attempt kinds.
const (
	KindRun    = "run"    // checked the visible tests
	KindSubmit = "submit" // checked every test; only a passed submit is progress
	KindHint   = "hint"
	KindQuiz   = "quiz" // answered a lesson's quiz question
)

// Attempt is one learner action on a task or lesson, written by the app only.
type Attempt struct {
	TaskID    string    `json:"taskId,omitempty"`
	Kind      string    `json:"kind"`
	LessonID  string    `json:"lessonId,omitempty"`
	Question  int       `json:"question,omitempty"` // quiz: question index
	Choice    int       `json:"choice,omitempty"`   // quiz: option picked
	Status    string    `json:"status,omitempty"`
	Passed    int       `json:"passed,omitempty"` // tests passed; quiz: 1 if correct
	Total     int       `json:"total,omitempty"`
	HintLevel int       `json:"hintLevel,omitempty"`
	At        time.Time `json:"at"`
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
func (a Attempt) passedTask() bool { return a.Kind == KindSubmit && a.Status == runner.StatusPassed }

// TaskStatus is a task plus whether the learner has passed it, and the
// deepest hint they've asked for (0 for none).
type TaskStatus struct {
	TaskInfo
	Passed    bool `json:"passed"`
	HintLevel int  `json:"hintLevel"`
}

func (w *Workspace) TaskStatuses() ([]TaskStatus, error) {
	tasks, err := w.Tasks()
	if err != nil {
		return nil, err
	}
	h := w.history()
	out := make([]TaskStatus, len(tasks))
	for i, t := range tasks {
		out[i] = TaskStatus{TaskInfo: t, Passed: h.passed[t.ID], HintLevel: h.hints[t.ID].Highest}
	}
	return out, nil
}

// Objective status values.
const (
	ObjectivePlanned    = "planned"     // no task yet
	ObjectiveInProgress = "in_progress" // has a task, none passed
	ObjectivePassed     = "passed"      // a task for it passed on submit
)

type ObjectiveProgress struct {
	ID     string   `json:"id"`
	Status string   `json:"status"`
	Tasks  []string `json:"tasks"`
}

type Progress struct {
	Passed     int                          `json:"passed"`
	Total      int                          `json:"total"`
	Objectives map[string]ObjectiveProgress `json:"objectives"`
	// PassedEver includes objectives dropped from the plan, so changing the
	// plan never looks like losing progress.
	PassedEver int `json:"passedEver"`
}

// Progress is derived from tasks and attempts; nothing the agent writes
// can change it.
func (w *Workspace) Progress() Progress {
	p := Progress{Objectives: map[string]ObjectiveProgress{}}
	tasks, _ := w.Tasks()
	passedTask := w.history().passed
	byObjective := map[string][]string{}
	passedObjective := map[string]bool{}
	for _, t := range tasks {
		byObjective[t.Objective] = append(byObjective[t.Objective], t.ID)
		if passedTask[t.ID] {
			passedObjective[t.Objective] = true
		}
	}
	p.PassedEver = len(passedObjective)

	cur, _ := w.Curriculum()
	if cur == nil {
		return p
	}
	for _, m := range cur.Modules {
		for _, o := range m.Objectives {
			op := ObjectiveProgress{ID: o.ID, Status: ObjectivePlanned, Tasks: byObjective[o.ID]}
			if op.Tasks == nil {
				op.Tasks = []string{}
			}
			switch {
			case passedObjective[o.ID]:
				op.Status = ObjectivePassed
				p.Passed++
			case len(op.Tasks) > 0:
				op.Status = ObjectiveInProgress
			}
			p.Objectives[o.ID] = op
			p.Total++
		}
	}
	return p
}

// NeedsWriting returns the objectives a writer should prepare: of the next
// ahead unpassed objectives in plan order, the ones missing a lesson or a
// task the learner hasn't solved yet.
func (w *Workspace) NeedsWriting(ahead int) []Objective {
	c, err := w.Curriculum()
	if err != nil || c == nil {
		return nil
	}
	tasks, _ := w.TaskStatuses()
	lessons, _ := w.LessonStatuses()
	progress := w.Progress()
	var out []Objective
	for _, m := range c.Modules {
		for _, o := range m.Objectives {
			if progress.Objectives[o.ID].Status == ObjectivePassed {
				continue
			}
			if ahead == 0 {
				return out
			}
			ahead--
			hasLesson := slices.ContainsFunc(lessons, func(l LessonStatus) bool { return l.Objective == o.ID })
			hasTask := slices.ContainsFunc(tasks, func(t TaskStatus) bool { return t.Objective == o.ID && !t.Passed })
			if !hasLesson || !hasTask {
				out = append(out, o)
			}
		}
	}
	return out
}

func (w *Workspace) TaskPassed(id string) bool { return w.history().passed[id] }

// HintUse is how many hints the learner asked for on a task, and the deepest.
type HintUse struct{ Count, Highest int }

// Hints reports the hints the learner asked for on a task.
func (w *Workspace) Hints(id string) HintUse { return w.history().hints[id] }

// history is what the attempt log says about each task and lesson.
type history struct {
	passed map[string]bool               // task id -> passed
	hints  map[string]HintUse            // task id -> hints asked for
	quiz   map[string]map[int]QuizResult // lesson id -> first answer per question
}

func (w *Workspace) history() history {
	h := history{passed: map[string]bool{}, hints: map[string]HintUse{}, quiz: map[string]map[int]QuizResult{}}
	attempts, _ := w.Attempts()
	for _, a := range attempts {
		switch {
		case a.passedTask():
			h.passed[a.TaskID] = true
		case a.Kind == KindHint:
			u := h.hints[a.TaskID]
			h.hints[a.TaskID] = HintUse{Count: u.Count + 1, Highest: max(u.Highest, a.HintLevel)}
		case a.Kind == KindQuiz:
			if h.quiz[a.LessonID] == nil {
				h.quiz[a.LessonID] = map[int]QuizResult{}
			}
			if _, seen := h.quiz[a.LessonID][a.Question]; !seen {
				h.quiz[a.LessonID][a.Question] = QuizResult{Choice: a.Choice, Correct: a.Passed == 1}
			}
		}
	}
	return h
}

// nextTask is the title of the first task the learner hasn't passed.
func (w *Workspace) nextTask() string {
	tasks, _ := w.TaskStatuses()
	for _, t := range tasks {
		if !t.Passed {
			return t.Title
		}
	}
	return ""
}
