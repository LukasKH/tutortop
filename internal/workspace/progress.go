package workspace

import (
	"os"
	"strconv"
	"strings"
)

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

// Complete reports whether the learner passed every objective of the plan.
// A plan that gains objectives isn't complete any more.
func (p Progress) Complete() bool { return p.Total > 0 && p.Passed == p.Total }

// Progress is derived from tasks and attempts; nothing the agent writes
// can change it.
func (w *Workspace) Progress() Progress {
	cur, _ := w.Curriculum()
	tasks, _ := w.Tasks()
	return progress(cur, tasks, w.history().passed)
}

// NeedsWriting returns the objectives a writer should prepare: of the next
// ahead unpassed objectives in plan order, the ones missing a lesson or a
// task the learner hasn't solved yet.
func (w *Workspace) NeedsWriting(ahead int) []Objective {
	cur, err := w.Curriculum()
	if err != nil || cur == nil {
		return nil
	}
	tasks, _ := w.Tasks()
	lessons, _ := w.Lessons()
	p := progress(cur, tasks, w.history().passed)
	hasLesson := map[string]bool{}
	for _, l := range lessons {
		hasLesson[l.Objective] = true
	}
	var out []Objective
	for _, m := range cur.Modules {
		for _, o := range m.Objectives {
			status := p.Objectives[o.ID].Status
			if status == ObjectivePassed {
				continue
			}
			if ahead == 0 {
				return out
			}
			ahead--
			// None of an unpassed objective's tasks is solved, so it has an
			// unsolved one unless it has none at all.
			if !hasLesson[o.ID] || status == ObjectivePlanned {
				out = append(out, o)
			}
		}
	}
	return out
}

// ClearUnsolved removes already-written lessons (except keepLessonID) and tasks
// belonging to unpassed objectives, so writers can rewrite them after feedback.
// Passed objectives and keepLessonID are untouched. It returns the objectives
// that had content removed, in plan order.
func (w *Workspace) ClearUnsolved(keepLessonID string) ([]Objective, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	cur, err := w.Curriculum()
	if err != nil || cur == nil {
		return nil, err
	}
	tasks, err := w.Tasks()
	if err != nil {
		return nil, err
	}
	lessons, err := w.Lessons()
	if err != nil {
		return nil, err
	}
	unpassed := unpassedObjectives(cur, progress(cur, tasks, w.history().passed))
	affected := map[string]bool{}
	for _, l := range lessons {
		if !unpassed[l.Objective] || l.ID == keepLessonID {
			continue
		}
		if err := w.removeLesson(l.ID); err != nil {
			return nil, err
		}
		affected[l.Objective] = true
	}
	for _, t := range tasks {
		if !unpassed[t.Objective] {
			continue
		}
		if err := w.removeTask(t.ID); err != nil {
			return nil, err
		}
		affected[t.Objective] = true
	}
	cleared := objectivesInOrder(cur, affected)
	if len(cleared) > 0 {
		w.commit("Regenerate unsolved lessons and exercises")
	}
	return cleared, nil
}

func (w *Workspace) removeLesson(id string) error {
	if err := removeAll(w.path("lessons", id), w.private("lessons", id)); err != nil {
		return err
	}
	name := LessonThread(id).String()
	_ = removeAll(w.path(".tutortop", "chat", name+".jsonl"), w.path(".tutortop", "chat", name))
	return nil
}

func (w *Workspace) removeTask(id string) error {
	if err := removeAll(w.path("tasks", id), w.private("tasks", id)); err != nil {
		return err
	}
	name := TaskThread(id).String()
	_ = removeAll(w.path(".tutortop", "chat", name+".jsonl"), w.path(".tutortop", "chat", name))
	return nil
}

func removeAll(paths ...string) error {
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}

func unpassedObjectives(cur *Curriculum, p Progress) map[string]bool {
	out := map[string]bool{}
	for _, m := range cur.Modules {
		for _, o := range m.Objectives {
			if p.Objectives[o.ID].Status != ObjectivePassed {
				out[o.ID] = true
			}
		}
	}
	return out
}

func objectivesInOrder(cur *Curriculum, want map[string]bool) []Objective {
	var out []Objective
	for _, m := range cur.Modules {
		for _, o := range m.Objectives {
			if want[o.ID] {
				out = append(out, o)
			}
		}
	}
	return out
}

// nextNumber returns one more than the highest numeric prefix ("0004-...")
// among ids, so removing an earlier unsolved lesson or task never causes a new
// one to collide with a higher-numbered one.
func nextNumber(ids []string) int {
	highest := 0
	for _, id := range ids {
		prefix, _, _ := strings.Cut(id, "-")
		if n, err := strconv.Atoi(prefix); err == nil && n > highest {
			highest = n
		}
	}
	return highest + 1
}

func (w *Workspace) TaskPassed(id string) bool { return w.history().passed[id] }

// HintUse is how many hints the learner asked for on a task, and the deepest.
type HintUse struct{ Count, Highest int }

// Hints reports the hints the learner asked for on a task.
func (w *Workspace) Hints(id string) HintUse { return w.history().hints[id] }

// progress works out how far the learner got with each objective of the
// plan, from the tasks and which of them passed. cur may be nil.
func progress(cur *Curriculum, tasks []TaskInfo, passedTask map[string]bool) Progress {
	p := Progress{Objectives: map[string]ObjectiveProgress{}}
	byObjective := map[string][]string{}
	passedObjective := map[string]bool{}
	for _, t := range tasks {
		byObjective[t.Objective] = append(byObjective[t.Objective], t.ID)
		if passedTask[t.ID] {
			passedObjective[t.Objective] = true
		}
	}
	p.PassedEver = len(passedObjective)
	if cur == nil {
		return p
	}
	for _, m := range cur.Modules {
		for _, o := range m.Objectives {
			op := ObjectiveProgress{ID: o.ID, Status: ObjectivePlanned, Tasks: byObjective[o.ID]}
			if len(op.Tasks) == 0 {
				op.Tasks = []string{} // [] in JSON, not null
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
