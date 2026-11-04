package app

import (
	"cmp"
	_ "embed"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/text"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// What the agent is told besides the learner's words: its instructions, set
// when its process starts, and a [context] block on each of the learner's
// messages about what they have open. Both depend on the kind of thread.

// The agent's instructions: shared rules plus one role per kind of thread.
var (
	//go:embed prompts/shared.md
	sharedPrompt string
	//go:embed prompts/plan.md
	planRole string
	//go:embed prompts/task.md
	taskRole string
	//go:embed prompts/lesson.md
	lessonRole string
	//go:embed prompts/writer.md
	writerRole string
)

// instructions are the shared rules plus the agent's role in the thread. A
// title that can't be read falls back to the id.
func instructions(w *workspace.Workspace, t workspace.Thread) string {
	var role, title string
	switch t.Kind {
	case workspace.PlanKind:
		role = planRole
	case workspace.TaskKind:
		d, _ := w.Task(t.ID)
		role, title = taskRole, d.Title
	case workspace.LessonKind:
		d, _ := w.Lesson(t.ID)
		role, title = lessonRole, d.Title
	case workspace.WriterKind:
		role = writerRole
		if c, err := w.Curriculum(); err == nil && c != nil {
			o, _, _ := c.Objective(t.ID)
			title = o.Title
		}
	}
	return sharedPrompt + "\n\n" + strings.NewReplacer("{{id}}", t.ID, "{{title}}", cmp.Or(title, t.ID)).Replace(role)
}

// contextFor describes what the learner has open in the thread: a task's
// code and last check, or how a lesson's quiz went. The plan and writers get
// none.
func (a *App) contextFor(w *workspace.Workspace, t workspace.Thread) string {
	switch t.Kind {
	case workspace.TaskKind:
		a.mu.Lock()
		last, ok := a.lastResults[taskKey{w.ID, t.ID}]
		a.mu.Unlock()
		if !ok {
			return taskContext(w, t.ID, nil)
		}
		return taskContext(w, t.ID, &last)
	case workspace.LessonKind:
		return lessonContext(w, t.ID)
	}
	return ""
}

// taskContext tells a task's tutor the learner's current code and, if they
// ran it, the last check.
func taskContext(w *workspace.Workspace, id string, last *runner.Result) string {
	d, err := w.Task(id)
	if err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[context]\nOpen task: %s (%s), objective %s, language %s.\n", d.ID, d.Title, d.Objective, d.Language)
	for _, name := range slices.Sorted(maps.Keys(d.Work)) {
		fmt.Fprintf(&b, "Learner's current %s:\n```\n%s\n```\n", name, d.Work[name])
	}
	if last != nil {
		p, t := last.Counts()
		fmt.Fprintf(&b, "Last check: %s, %d/%d tests passing.\n", last.Status, p, t)
		for _, tr := range last.Tests {
			if !tr.Passed {
				kind := "visible"
				if tr.Hidden {
					kind = "hidden"
				}
				fmt.Fprintf(&b, "- %s (%s) failed: %s\n", tr.Name, kind, text.Truncate(tr.Message, 400))
			}
		}
		if last.Status == runner.StatusError {
			fmt.Fprintf(&b, "Output:\n%s\n", text.Truncate(last.Output, 1500))
		}
	}
	b.WriteString("[/context]\n\n")
	return b.String()
}

// lessonContext tells a lesson's tutor how the learner is doing on its quiz.
func lessonContext(w *workspace.Workspace, id string) string {
	d, err := w.Lesson(id)
	if err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[context]\nOpen lesson: %s (%s), objective %s. Quiz: %d of %d answered, %d correct.\n", d.ID, d.Title, d.Objective, d.Answered, d.Questions, d.Correct)
	for i, q := range d.Quiz {
		if r, ok := d.Results[i]; ok && !r.Correct {
			fmt.Fprintf(&b, "- Got question %d wrong: %q, picked %q.\n", i+1, text.Truncate(q.Question, 300), q.Options[r.Choice])
		}
	}
	b.WriteString("[/context]\n\n")
	return b.String()
}
