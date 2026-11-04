package workspace

import "strings"

// A Thread is one conversation with the agent in a workspace. Its name
// (String) is the chat's file name, the key of its saved agent session and
// what the frontend sends and receives, so the forms below must not change:
//
//	"plan"                   the planning conversation: interview, mission and curriculum
//	"<task id>"              help with one task
//	"lesson-<lesson id>"     questions about one lesson
//	"write-<objective id>"   the writer of one objective's lesson and exercise, in the background
type Thread struct {
	Kind ThreadKind
	ID   string // the task, lesson or objective it's about; "" for the plan
}

// ParseThread reads a thread's name. "" is the plan too, as the frontend and
// question forms leave it out for the plan. Anything else that isn't a lesson
// or a writer is a task id: task ids are numbered, so they never start with
// "lesson-" or "write-".
func ParseThread(name string) Thread {
	if id, ok := strings.CutPrefix(name, "lesson-"); ok {
		return LessonThread(id)
	}
	if id, ok := strings.CutPrefix(name, "write-"); ok {
		return WriterThread(id)
	}
	if name == "plan" || name == "" {
		return PlanThread
	}
	return TaskThread(name)
}

// PlanThread is the planning conversation.
var PlanThread = Thread{Kind: PlanKind}

// TaskThread is the chat about one task.
func TaskThread(id string) Thread { return Thread{TaskKind, id} }

// LessonThread is the chat about one lesson.
func LessonThread(id string) Thread { return Thread{LessonKind, id} }

// WriterThread is the conversation that writes one objective.
func WriterThread(objectiveID string) Thread { return Thread{WriterKind, objectiveID} }

// String returns the thread's name.
func (t Thread) String() string {
	switch t.Kind {
	case PlanKind:
		return "plan"
	case LessonKind:
		return "lesson-" + t.ID
	case WriterKind:
		return "write-" + t.ID
	}
	return t.ID
}

type ThreadKind int

const (
	PlanKind ThreadKind = iota
	TaskKind
	LessonKind
	// WriterKind threads aren't shown as chats. The learner doesn't talk in
	// them, and each ends when its writing is done.
	WriterKind
)
