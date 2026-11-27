package workspace

import (
	"cmp"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
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
// Passed objectives and keepLessonID are untouched. Before deleting anything it
// advances the writer generation (so stopped writers cannot save stale work),
// records monotonic ID counters, and commits any uncommitted learner code to
// git history. Removals are staged via rename so a failure rolls back cleanly.
func (w *Workspace) ClearUnsolved(keepLessonID string) ([]Objective, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state.gen++

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
	removeLessons, removeTasks, affected := selectUnsolved(lessons, tasks, unpassed, keepLessonID)
	if len(affected) == 0 {
		return nil, nil
	}

	// Preserve uncommitted learner code in git history before replacing tasks.
	w.commit("Save work before regenerating unsolved exercises")

	staged, err := w.stageRemovals(removeLessons, removeTasks)
	if err != nil {
		rollbackMoves(staged)
		return nil, err
	}
	// Advance persistent ID counters and forget removed threads' resume IDs so
	// rewritten items never inherit old quiz answers, hints or chat sessions.
	if err := w.bumpCountersAndForgetThreads(lessons, tasks, removeLessons, removeTasks); err != nil {
		rollbackMoves(staged)
		return nil, err
	}
	// Delete chats only after metadata is saved, so a rolled-back failure never
	// erases chats belonging to restored lessons or exercises.
	w.removeChats(removeLessons, removeTasks)
	cleared := objectivesInOrder(cur, affected)
	cleanErr := cleanupStaged(staged)
	w.commit("Regenerate unsolved lessons and exercises")
	return cleared, cleanErr
}

type stagedMove struct{ orig, staged string }

func selectUnsolved(lessons []LessonInfo, tasks []TaskInfo, unpassed map[string]bool, keepLessonID string) ([]LessonInfo, []TaskInfo, map[string]bool) {
	var rmLessons []LessonInfo
	var rmTasks []TaskInfo
	affected := map[string]bool{}
	for _, l := range lessons {
		if unpassed[l.Objective] && l.ID != keepLessonID {
			rmLessons = append(rmLessons, l)
			affected[l.Objective] = true
		}
	}
	for _, t := range tasks {
		if unpassed[t.Objective] {
			rmTasks = append(rmTasks, t)
			affected[t.Objective] = true
		}
	}
	return rmLessons, rmTasks, affected
}

// stageRemovals moves every folder to be deleted into gitignored/private temp
// folders on the same filesystem so an error midway can roll back atomically.
func (w *Workspace) stageRemovals(lessons []LessonInfo, tasks []TaskInfo) ([]stagedMove, error) {
	tag := rand.Text()
	// Public trash lives under .tutortop/chat/ (already in .gitignore) so a
	// partial cleanup error never gets committed into the workspace repo.
	pubTrash := w.path(".tutortop", "chat", ".regen-"+tag)
	privTrash := w.private(".regen-" + tag)
	var moves []stagedMove
	stage := func(orig, trashDir, name string) error {
		if _, err := os.Stat(orig); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		if err := os.MkdirAll(trashDir, 0o755); err != nil {
			return err
		}
		dst := filepath.Join(trashDir, name)
		if err := os.Rename(orig, dst); err != nil {
			return err
		}
		moves = append(moves, stagedMove{orig: orig, staged: dst})
		return nil
	}
	for _, l := range lessons {
		if err := stage(w.path("lessons", l.ID), pubTrash, "lesson-pub-"+l.ID); err != nil {
			return moves, err
		}
		if err := stage(w.private("lessons", l.ID), privTrash, "lesson-priv-"+l.ID); err != nil {
			return moves, err
		}
	}
	for _, t := range tasks {
		if err := stage(w.path("tasks", t.ID), pubTrash, "task-pub-"+t.ID); err != nil {
			return moves, err
		}
		if err := stage(w.private("tasks", t.ID), privTrash, "task-priv-"+t.ID); err != nil {
			return moves, err
		}
	}
	return moves, nil
}

func (w *Workspace) removeChats(lessons []LessonInfo, tasks []TaskInfo) {
	for _, l := range lessons {
		name := LessonThread(l.ID).String()
		_ = removeAll(w.path(".tutortop", "chat", name+".jsonl"), w.path(".tutortop", "chat", name))
	}
	for _, t := range tasks {
		name := TaskThread(t.ID).String()
		_ = removeAll(w.path(".tutortop", "chat", name+".jsonl"), w.path(".tutortop", "chat", name))
	}
}

func rollbackMoves(moves []stagedMove) {
	for i := len(moves) - 1; i >= 0; i-- {
		_ = os.Rename(moves[i].staged, moves[i].orig)
	}
	for _, m := range moves {
		_ = os.Remove(filepath.Dir(m.staged))
	}
}

func cleanupStaged(moves []stagedMove) error {
	var firstErr error
	dirs := map[string]bool{}
	for _, m := range moves {
		dirs[filepath.Dir(m.staged)] = true
		if err := os.RemoveAll(m.staged); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for d := range dirs {
		_ = os.Remove(d)
	}
	return firstErr
}

func removeAll(paths ...string) error {
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}

// claimLessonNumber returns a never-reused lesson number and persists the
// updated counter in workspace.json. Caller holds w.mu.
func (w *Workspace) claimLessonNumber(lessons []LessonInfo) (int, error) {
	meta, err := readJSON[Meta](w.path(".tutortop", "workspace.json"))
	if err != nil {
		return 0, err
	}
	n := max(meta.NextLesson, w.minNextLesson(lessons, meta))
	meta.NextLesson = n + 1
	w.Meta = meta
	return n, w.saveMeta()
}

// claimTaskNumber returns a never-reused task number and persists the updated
// counter in workspace.json. Caller holds w.mu.
func (w *Workspace) claimTaskNumber(tasks []TaskInfo) (int, error) {
	meta, err := readJSON[Meta](w.path(".tutortop", "workspace.json"))
	if err != nil {
		return 0, err
	}
	n := max(meta.NextTask, w.minNextTask(tasks, meta))
	meta.NextTask = n + 1
	w.Meta = meta
	return n, w.saveMeta()
}

func (w *Workspace) bumpCountersAndForgetThreads(allLessons []LessonInfo, allTasks []TaskInfo, rmLessons []LessonInfo, rmTasks []TaskInfo) error {
	meta, err := readJSON[Meta](w.path(".tutortop", "workspace.json"))
	if err != nil {
		return err
	}
	meta.NextLesson = max(meta.NextLesson, w.minNextLesson(allLessons, meta))
	meta.NextTask = max(meta.NextTask, w.minNextTask(allTasks, meta))
	for _, l := range rmLessons {
		delete(meta.ResumeIDs, LessonThread(l.ID).String())
	}
	for _, t := range rmTasks {
		delete(meta.ResumeIDs, TaskThread(t.ID).String())
	}
	w.Meta = meta
	return w.saveMeta()
}

func (w *Workspace) minNextLesson(lessons []LessonInfo, meta Meta) int {
	ids := make([]string, 0, len(lessons))
	for _, l := range lessons {
		ids = append(ids, l.ID)
	}
	attempts, _ := w.Attempts()
	for _, a := range attempts {
		if a.LessonID != "" {
			ids = append(ids, a.LessonID)
		}
	}
	for thread := range meta.ResumeIDs {
		if id, ok := strings.CutPrefix(thread, "lesson-"); ok {
			ids = append(ids, id)
		}
	}
	return nextNumber(ids)
}

func (w *Workspace) minNextTask(tasks []TaskInfo, meta Meta) int {
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.ID)
	}
	attempts, _ := w.Attempts()
	for _, a := range attempts {
		if a.TaskID != "" {
			ids = append(ids, a.TaskID)
		}
	}
	for thread := range meta.ResumeIDs {
		if t, err := ParseThread(thread); err == nil && t.Kind == TaskKind {
			ids = append(ids, t.ID)
		}
	}
	return nextNumber(ids)
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

// nextTask is what's next for the learner in plan order: the first unpassed
// objective's newest unsolved task title, else its lesson title, else its
// objective name or title. Returns "" when there is no plan or every objective
// is passed.
func (w *Workspace) nextTask() string {
	cur, err := w.Curriculum()
	if err != nil || cur == nil {
		return ""
	}
	tasks, _ := w.TaskStatuses()
	lessons, _ := w.Lessons()
	p := w.Progress()
	for _, m := range cur.Modules {
		for _, o := range m.Objectives {
			if p.Objectives[o.ID].Status == ObjectivePassed {
				continue
			}
			for i := len(tasks) - 1; i >= 0; i-- {
				if tasks[i].Objective == o.ID && !tasks[i].Passed {
					return tasks[i].Title
				}
			}
			for _, l := range lessons {
				if l.Objective == o.ID {
					return l.Title
				}
			}
			return cmp.Or(o.Name, o.Title)
		}
	}
	return ""
}
