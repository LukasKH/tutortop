package app

import (
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Feedback after an exercise or a lesson reaches the planning tutor as an
// [app] message in the learner's words, comparisons named after what the
// mission says they know, and shows in the plan's chat.
func TestFeedbackReachesThePlanTutor(t *testing.T) {
	a, w, tutor := startPlanned(t)
	taskID, lessonID := addObjective(t, w)
	if _, err := w.SaveMission("# Mission\n\n## You already know\n\nPython\n"); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		send func(Feedback) error
		fb   Feedback
		want []string // in the agent's message, in order
	}{
		{
			name: "exercise",
			send: func(fb Feedback) error { return a.Tasks.SendFeedback(w.ID, taskID, fb) },
			fb:   Feedback{Chips: []FeedbackChip{FeedbackTooTechnical, FeedbackMoreComparisons}, Text: " Shorter statements, please. "},
			want: []string{"[app] My feedback on the exercise “Sum”:", "It was too technical.", "I'd like more comparisons to Python.", "“Shorter statements, please.”", "NOTES.md", "task_defaults"},
		},
		{
			name: "lesson",
			send: func(fb Feedback) error { return a.Lessons.SendFeedback(w.ID, lessonID, fb) },
			fb:   Feedback{Chips: []FeedbackChip{FeedbackClear, FeedbackRightLength}},
			want: []string{"[app] My feedback on the lesson “Adding numbers”:", "It was clear.", "It was the right length."},
		},
	} {
		if err := tc.send(tc.fb); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		turn := tutor.next(t, a, w, workspace.PlanThread)
		wantInOrder(t, tc.name, turn.prompt, tc.want)
		turn.reply("Noted.")
	}

	chat, err := a.Chat.Chat(w.ID, workspace.PlanThread.String())
	if err != nil {
		t.Fatal(err)
	}
	var shown []string
	for _, item := range chat {
		if item.Role == workspace.RoleNotice && strings.HasPrefix(item.Text, "Your feedback on") {
			shown = append(shown, item.Text)
		}
	}
	if len(shown) != 2 || !strings.Contains(shown[0], "the exercise “Sum”") || !strings.Contains(shown[1], "the lesson “Adding numbers”") {
		t.Errorf("feedback notices in the plan's chat: %q", shown)
	}
	idle(t, a, w.ID)
	tutor.noMoreTurns(t)
}

// Without anything to say, or with a chip the app doesn't know, feedback is
// refused and the tutor isn't bothered.
func TestFeedbackNeedsSomethingToSay(t *testing.T) {
	a, w, tutor := startPlanned(t)
	taskID, _ := addObjective(t, w)
	for _, fb := range []Feedback{
		{Text: "   "},
		{Chips: []FeedbackChip{"tooShiny"}},
	} {
		if err := a.Tasks.SendFeedback(w.ID, taskID, fb); err == nil {
			t.Errorf("feedback %+v was sent", fb)
		}
	}
	// Without a mission that says what they know, comparisons stay general.
	if err := a.Tasks.SendFeedback(w.ID, taskID, Feedback{Chips: []FeedbackChip{FeedbackMoreComparisons}}); err != nil {
		t.Fatal(err)
	}
	turn := tutor.next(t, a, w, workspace.PlanThread)
	wantInOrder(t, "comparisons", turn.prompt, []string{"I'd like more comparisons to what I already know."})
	turn.reply("Noted.")
	idle(t, a, w.ID)
	tutor.noMoreTurns(t)
}

// Feedback the planning tutor can't take fails, so the learner can send it
// again, instead of being thanked for feedback the tutor never gets.
func TestFeedbackTheTutorCantTakeFails(t *testing.T) {
	a, w, tutor := startPlanned(t)
	taskID, _ := addObjective(t, w)
	fb := Feedback{Chips: []FeedbackChip{FeedbackTooHard}}
	if err := a.Tasks.SendFeedback(w.ID, taskID, fb); err != nil {
		t.Fatal(err)
	}
	busy := tutor.next(t, a, w, workspace.PlanThread)
	// The tutor is busy with the first: fill its queue.
	waiting := 0
	for {
		err := a.Tasks.SendFeedback(w.ID, taskID, fb)
		if err != nil {
			break
		}
		waiting++
		if waiting > 100 {
			t.Fatal("the tutor's queue never filled")
		}
	}
	busy.reply("Noted.")
	for range waiting {
		tutor.next(t, a, w, workspace.PlanThread).reply("Noted.")
	}
	idle(t, a, w.ID)
	tutor.noMoreTurns(t)
}

// The learner sees how they like to learn on the plan page and edits it
// there. A change tells the planning tutor; saving the same text doesn't.
func TestLearnerEditsNotes(t *testing.T) {
	a, w, tutor := startPlanned(t)
	if err := a.Workspaces.SaveNotes(w.ID, "- Compare Go to Python.\n"); err != nil {
		t.Fatal(err)
	}
	turn := tutor.next(t, a, w, workspace.PlanThread)
	wantInOrder(t, "notes edit", turn.prompt, []string{"[app] I edited my notes on how I like to learn by hand."})
	turn.reply("Got it.")
	if st := idle(t, a, w.ID); st.Notes != "- Compare Go to Python.\n" {
		t.Errorf("snapshot notes %q", st.Notes)
	}
	if err := a.Workspaces.SaveNotes(w.ID, "- Compare Go to Python."); err != nil {
		t.Fatal(err)
	}
	idle(t, a, w.ID)
	tutor.noMoreTurns(t)
}

// startPlanned starts the app on a new workspace whose interview turn the
// test has answered, so the plan tutor's next turn is the test's.
func startPlanned(t *testing.T) (*testApp, *workspace.Workspace, *tutorCLI) {
	t.Helper()
	tutor := &tutorCLI{turns: make(chan *turn, 8)}
	a := newTestAppWith(t, tempDirs(t), tutor)
	id, err := a.Workspaces.CreateWorkspace("Learn Go", "")
	if err != nil {
		t.Fatal(err)
	}
	w, err := a.store.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	tutor.next(t, a, w, workspace.PlanThread).reply("Let's start.")
	idle(t, a, id)
	return a, w, tutor
}

// addObjective approves a one-objective plan and saves its lesson and task,
// as if their writer had, and returns their ids.
func addObjective(t *testing.T, w *workspace.Workspace) (taskID, lessonID string) {
	t.Helper()
	plan := workspace.Curriculum{Language: "go", Modules: []workspace.Module{{
		ID:         "basics",
		Title:      "Basics",
		References: []workspace.Source{{Title: "Go docs", URL: "https://go.dev/doc/"}},
		Objectives: []workspace.Objective{{ID: "go.sum", Name: "Adding", Title: "Add two numbers"}},
	}}}
	if err := w.Propose(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
	lesson, err := w.SaveLesson(workspace.LessonSpec{
		ObjectiveID:   "go.sum",
		Title:         "Adding numbers",
		Summary:       "You'll add two numbers in Go.",
		Body:          "## Adding\n\nUse `+`, as [the spec](https://go.dev/ref/spec#Arithmetic_operators) says.\n",
		PrimarySource: workspace.Source{Title: "The Go spec", URL: "https://go.dev/ref/spec"},
		Quiz: []workspace.QuizQuestion{
			{Question: "What is 1 + 2?", Options: []string{"three", "four", "five"}, Answer: 0, Explanation: "One plus two is three."},
			{Question: "What is 2 + 2?", Options: []string{"three", "four", "five"}, Answer: 1, Explanation: "Two plus two is four."},
			{Question: "What is 2 + 3?", Options: []string{"three", "four", "five"}, Answer: 2, Explanation: "Two plus three is five."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := w.SaveTask(workspace.TaskSpec{
		ObjectiveID: "go.sum",
		Title:       "Sum",
		Statement: workspace.Statement{
			Task:         "Write Sum so that it returns a + b.",
			Signature:    "func Sum(a, b int) int",
			Requirements: []string{"Sum returns a + b."},
			Examples:     []workspace.Example{{Input: "Sum(1, 2)", Output: "3"}, {Input: "Sum(-1, 1)", Output: "0"}},
			Context:      "Totals in a budget.",
		},
		Starter:     sandbox.Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return 0 }\n"},
		Reference:   sandbox.Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a + b }\n"},
		Tests:       sandbox.Files{"sum_test.go": "package task\n"},
		HiddenTests: sandbox.Files{"sum_hidden_test.go": "package task\n"},
	}, "go", sandbox.TestNames{Visible: []string{"TestSum"}, Hidden: []string{"TestSumHidden"}})
	if err != nil {
		t.Fatal(err)
	}
	return task.ID, lesson.ID
}

// wantInOrder fails unless text holds each of want, in order.
func wantInOrder(t *testing.T, what, text string, want []string) {
	t.Helper()
	rest := text
	for _, s := range want {
		i := strings.Index(rest, s)
		if i < 0 {
			t.Errorf("%s: %q missing (or out of order) in %q", what, s, text)
			return
		}
		rest = rest[i+len(s):]
	}
}
