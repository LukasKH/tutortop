package app

import (
	"strings"
	"testing"
	"time"

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

// Feedback with Regenerate waits until the planning tutor finishes updating
// NOTES.md, then clears already-written unsolved lessons and tasks (leaving
// passed objectives untouched) and starts fresh writers for them.
func TestFeedbackRegeneratesUnsolvedAfterNotesUpdate(t *testing.T) {
	a, w, tutor := startPlanned(t)
	plan := workspace.Curriculum{Language: "go", Modules: []workspace.Module{{
		ID:         "basics",
		Title:      "Basics",
		References: []workspace.Source{{Title: "Go docs", URL: "https://go.dev/doc/"}},
		Objectives: []workspace.Objective{
			{ID: "go.one", Name: "One", Title: "First objective"},
			{ID: "go.two", Name: "Two", Title: "Second objective"},
			{ID: "go.three", Name: "Three", Title: "Third objective"},
		},
	}}}
	if err := w.Propose(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
	t1, l1 := saveSampleObjective(t, w, "go.one", "One")
	saveSampleObjective(t, w, "go.two", "Two")
	saveSampleObjective(t, w, "go.three", "Three")
	if err := w.RecordAttempt(workspace.Attempt{TaskID: t1, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed}); err != nil {
		t.Fatal(err)
	}

	if err := a.Tasks.SendFeedback(w.ID, t1, Feedback{
		Chips:      []FeedbackChip{FeedbackTooTechnical},
		Text:       "Use plainer words.",
		Regenerate: true,
	}); err != nil {
		t.Fatal(err)
	}
	planTurn := tutor.next(t, a, w, workspace.PlanThread)
	wantInOrder(t, "regenerate prompt", planTurn.prompt, []string{"It was too technical.", "“Use plainer words.”", "regenerate already-written unsolved lessons and exercises"})

	// Unsolved items stay on disk until the planner finishes updating NOTES.md.
	if lessons, _ := w.Lessons(); len(lessons) != 3 {
		t.Fatalf("lessons cleared before planner finished: got %d, want 3", len(lessons))
	}
	planTurn.reply("Updated NOTES.md.")
	waitPlanIdle(t, a, w.ID)

	// Once the planner finishes, go.two and go.three are cleared and rewritten.
	w2, w3 := workspace.WriterThread("go.two"), workspace.WriterThread("go.three")
	writerTurns := tutor.nextEach(t, a, w, w2, w3)
	writerTurns[w2].reply("Rewritten.")
	writerTurns[w3].reply("Rewritten.")
	lessons, _ := w.Lessons()
	tasks, _ := w.Tasks()
	if len(lessons) != 1 || lessons[0].ID != l1 {
		t.Errorf("remaining lessons %+v, want only passed %s", lessons, l1)
	}
	if len(tasks) != 1 || tasks[0].ID != t1 {
		t.Errorf("remaining tasks %+v, want only passed %s", tasks, t1)
	}
	idle(t, a, w.ID)
	tutor.noMoreTurns(t)
}

// A "Write it now" writer on a later objective that hasn't published yet is
// restarted after regeneration, and the stopped writer cannot publish stale
// content through its old MCP endpoint.
func TestFeedbackRegenerateRestartsWriteNowAndBlocksStaleWriter(t *testing.T) {
	a, w, tutor := startPlanned(t)
	plan := workspace.Curriculum{Language: "go", Modules: []workspace.Module{{
		ID:         "basics",
		Title:      "Basics",
		References: []workspace.Source{{Title: "Go docs", URL: "https://go.dev/doc/"}},
		Objectives: []workspace.Objective{
			{ID: "go.one", Name: "One", Title: "First objective"},
			{ID: "go.two", Name: "Two", Title: "Second objective"},
			{ID: "go.three", Name: "Three", Title: "Third objective"},
			{ID: "go.four", Name: "Four", Title: "Fourth objective"},
		},
	}}}
	if err := w.Propose(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
	t1, _ := saveSampleObjective(t, w, "go.one", "One")
	if err := w.RecordAttempt(workspace.Attempt{TaskID: t1, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed}); err != nil {
		t.Fatal(err)
	}

	// Learner explicitly asked to write objective 4 (beyond the 2-ahead lookahead).
	if err := a.Workspaces.WriteObjectiveNow(w.ID, "go.four"); err != nil {
		t.Fatal(err)
	}
	w4 := workspace.WriterThread("go.four")
	staleTurn := tutor.next(t, a, w, w4)
	staleMCPURL := staleTurn.opts.MCPURL

	if err := a.Tasks.SendFeedback(w.ID, t1, Feedback{
		Chips:      []FeedbackChip{FeedbackTooTechnical},
		Regenerate: true,
	}); err != nil {
		t.Fatal(err)
	}
	tutor.next(t, a, w, workspace.PlanThread).reply("Updated NOTES.md.")
	waitPlanIdle(t, a, w.ID)

	// Rewrite stops stale go.four and restarts go.two, go.three (KeepReady) AND go.four!
	w2, w3 := workspace.WriterThread("go.two"), workspace.WriterThread("go.three")
	restarted := tutor.nextEach(t, a, w, w2, w3, w4)
	if restarted[w4].opts.MCPURL == staleMCPURL {
		t.Error("replacement writer reused the stopped writer's MCP endpoint")
	}
	for _, tn := range restarted {
		tn.reply("Done.")
	}
	idle(t, a, w.ID)
	tutor.noMoreTurns(t)
}

func waitPlanIdle(t *testing.T, a *testApp, wsID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err := a.Workspaces.Workspace(wsID)
		if err != nil {
			t.Fatal(err)
		}
		if !st.Busy[workspace.PlanThread.String()] {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("plan thread never went idle")
}

// saveSampleObjective saves a lesson and task for one objective in w.
func saveSampleObjective(t *testing.T, w *workspace.Workspace, objectiveID, title string) (taskID, lessonID string) {
	t.Helper()
	lesson, err := w.SaveLesson(workspace.LessonSpec{
		ObjectiveID:   objectiveID,
		Title:         title + " lesson",
		Summary:       "Summary for " + title,
		Body:          "## Intro\n\nSee [the spec](https://go.dev/ref/spec).\n```go\nvar x int\n```\n",
		PrimarySource: workspace.Source{Title: "The Go spec", URL: "https://go.dev/ref/spec"},
		Quiz: []workspace.QuizQuestion{
			{Question: "Q1?", Options: []string{"one", "two", "three"}, Answer: 0, Explanation: "One."},
			{Question: "Q2?", Options: []string{"one", "two", "three"}, Answer: 1, Explanation: "Two."},
			{Question: "Q3?", Options: []string{"one", "two", "three"}, Answer: 2, Explanation: "Three."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := w.SaveTask(workspace.TaskSpec{
		ObjectiveID: objectiveID,
		Title:       title + " task",
		Statement: workspace.Statement{
			Context:   "Context.",
			Task:      "Complete " + title + ".",
			Signature: "func F() int",
			Steps:     []string{"Return `1`."},
			Examples:  []workspace.Example{{Code: "F() // 1"}},
		},
		Starter:     sandbox.Files{"f.go": "package task\n\nfunc F() int { return 0 }\n"},
		Reference:   sandbox.Files{"f.go": "package task\n\nfunc F() int { return 1 }\n"},
		Tests:       sandbox.Files{"f_test.go": "package task\n"},
		HiddenTests: sandbox.Files{"f_hidden_test.go": "package task\n"},
	}, "go", sandbox.TestNames{Visible: []string{"TestF"}, Hidden: []string{"TestFHidden"}})
	if err != nil {
		t.Fatal(err)
	}
	return task.ID, lesson.ID
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
			Context:   "Totals in a budget.",
			Task:      "Complete `Sum` so it returns `a + b`.",
			Signature: "func Sum(a, b int) int",
			Steps:     []string{"Return `a + b`."},
			Examples:  []workspace.Example{{Code: "Sum(1, 2) // 3"}, {Code: "Sum(-1, 1) // 0"}},
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
