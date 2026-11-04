package workspace

import "slices"

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

// ObjectiveStatus is how far the learner got with an objective.
type ObjectiveStatus string

const (
	ObjectivePlanned    ObjectiveStatus = "planned"     // no task yet
	ObjectiveInProgress ObjectiveStatus = "in_progress" // has a task, none passed
	ObjectivePassed     ObjectiveStatus = "passed"      // a task for it passed on submit
)

type ObjectiveProgress struct {
	ID     string          `json:"id"`
	Status ObjectiveStatus `json:"status"`
	Tasks  []string        `json:"tasks"`
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
