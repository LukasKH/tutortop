package workspace_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// A new workspace never takes an id that's still in use, including one whose
// private files outlived its folder.
func TestCreatePicksFreeID(t *testing.T) {
	s := &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	create := func(title string) string {
		t.Helper()
		w, err := s.Create(title, "claude")
		if err != nil {
			t.Fatal(err)
		}
		return w.ID
	}
	if id := create("Learn Go!"); id != "learn-go" {
		t.Errorf("id %q, want learn-go", id)
	}
	if id := create("Learn Go"); id != "learn-go-2" {
		t.Errorf("second id %q, want learn-go-2", id)
	}
	if err := os.MkdirAll(filepath.Join(s.PrivateRoot, "rust"), 0o755); err != nil {
		t.Fatal(err)
	}
	if id := create("Rust"); id != "rust-2" {
		t.Errorf("id %q next to leftover private files, want rust-2", id)
	}
	if id := create("???"); id != "topic" {
		t.Errorf("id for a title without letters %q, want topic", id)
	}

	list, err := s.List()
	if err != nil || len(list) != 4 {
		t.Fatalf("listed %d workspaces (%v), want 4", len(list), err)
	}
}

// Ids, task ids, lesson ids and thread names become path elements, so none
// may step outside its folder.
func TestNamesStayInsideTheWorkspace(t *testing.T) {
	w := newWorkspace(t)
	s := &workspace.Store{Root: filepath.Dir(w.Dir), PrivateRoot: filepath.Dir(w.PrivateDir)}
	for _, bad := range []string{"", ".", "..", "../x", `a\b`} {
		if _, err := s.Open(bad); err == nil {
			t.Errorf("Open(%q) succeeded", bad)
		}
		if _, err := w.Task(bad); err == nil {
			t.Errorf("Task(%q) succeeded", bad)
		}
		if _, err := w.Lesson(bad); err == nil {
			t.Errorf("Lesson(%q) succeeded", bad)
		}
		if _, err := w.Chat(bad); err == nil {
			t.Errorf("Chat(%q) succeeded", bad)
		}
		if err := w.AppendChat(workspace.ChatItem{Thread: bad, Role: workspace.RoleUser}); err == nil {
			t.Errorf("AppendChat to %q succeeded", bad)
		}
		if _, err := workspace.ParseThread(bad); err == nil {
			t.Errorf("ParseThread(%q) succeeded", bad)
		}
		if _, err := w.Toolchain(bad); err == nil {
			t.Errorf("Toolchain(%q) succeeded", bad)
		}
	}
	if got := w.ReadDoc("../../etc/passwd"); got != "" {
		t.Errorf("ReadDoc read outside the workspace: %q", got)
	}
}

func TestChat(t *testing.T) {
	w := newWorkspace(t)
	items := []workspace.ChatItem{
		{ID: "1", Thread: workspace.PlanThread.String(), Role: workspace.RoleUser, Text: "hi"},
		{ID: "2", Thread: workspace.PlanThread.String(), Role: workspace.RoleAssistant, Text: "hello"},
		{ID: "3", Thread: "0001-task", Role: workspace.RoleNotice, Text: "elsewhere"},
	}
	for _, it := range items {
		if err := w.AppendChat(it); err != nil {
			t.Fatal(err)
		}
	}
	got, err := w.Chat(workspace.PlanThread.String())
	if err != nil || len(got) != 2 || got[0].Text != "hi" || got[1].Role != workspace.RoleAssistant {
		t.Fatalf("plan thread: %+v, %v", got, err)
	}
	if got, err := w.Chat("never-used"); err != nil || len(got) != 0 {
		t.Errorf("unused thread: %+v, %v", got, err)
	}
}

// Older files leave the plan's thread name empty; they read as "plan", the
// one name the app sends.
func TestOldFilesNameThePlan(t *testing.T) {
	w := newWorkspace(t)
	if err := os.MkdirAll(filepath.Join(w.Dir, ".tutortop", "chat"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"chat/plan.jsonl": `{"id":"1","role":"user","text":"hi"}` + "\n",
		"questions.json":  `{"questions":[{"id":"q","question":"Why?"}]}`,
	} {
		if err := os.WriteFile(filepath.Join(w.Dir, ".tutortop", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if chat, err := w.Chat("plan"); err != nil || len(chat) != 1 || chat[0].Thread != "plan" {
		t.Errorf("chat = %+v, %v", chat, err)
	}
	if q := w.PendingQuestions(); q == nil || q.Thread != "plan" {
		t.Errorf("questions = %+v", q)
	}
}

// A tutor still finishing after its workspace was deleted mustn't bring the
// folder back.
func TestAppendChatAfterDelete(t *testing.T) {
	w := newWorkspace(t)
	s := &workspace.Store{Root: filepath.Dir(w.Dir), PrivateRoot: filepath.Dir(w.PrivateDir)}
	if err := s.Delete(w.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.AppendChat(workspace.ChatItem{Thread: workspace.PlanThread.String(), Role: workspace.RoleAssistant}); err == nil {
		t.Error("appended to a deleted workspace")
	}
	if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
		t.Errorf("the deleted folder is back: %v", err)
	}
}

// Sessions belong to one agent: switching forgets them, and a late session
// id from the old agent is dropped.
func TestSessionsFollowTheProvider(t *testing.T) {
	w := newWorkspace(t) // claude
	if err := w.SetSession("claude", workspace.PlanThread.String(), "c1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.UseProvider("claude"); got[workspace.PlanThread.String()] != "c1" {
		t.Fatalf("same provider lost its sessions: %v", got)
	}
	if got, _ := w.UseProvider("codex"); len(got) != 0 {
		t.Fatalf("switching kept sessions: %v", got)
	}
	if err := w.SetSession("claude", workspace.PlanThread.String(), "late"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetSession("codex", "0001-task", "x1"); err != nil {
		t.Fatal(err)
	}
	got, _ := w.UseProvider("codex")
	if want := map[string]string{"0001-task": "x1"}; !maps.Equal(got, want) {
		t.Errorf("sessions %v, want %v", got, want)
	}
}

// A language the workspace set up before the app shipped it keeps the
// toolchain its tasks passed the quality gate with.
func TestOwnToolchainOutranksANewBuiltin(t *testing.T) {
	w := newWorkspace(t)
	if tc, err := w.Toolchain("python"); err != nil || tc.Name != "Python" {
		t.Fatalf("built-in python = %+v, %v", tc, err)
	}
	// As if python weren't built in when the tutor set it up.
	own := runner.ToolchainSpec{Language: "python", Name: "My Python", Dockerfile: "FROM python:3.12", TestCommand: "pytest --junitxml=/tmp/junit.xml", TestFiles: "test_*.py"}
	if err := w.SaveToolchain(own); err != nil {
		t.Fatal(err)
	}
	if tc, err := w.Toolchain("python"); err != nil || tc.Name != "My Python" {
		t.Fatalf("python = %+v, %v; want the workspace's own", tc, err)
	}
}

// ---- helpers shared by the package's tests ----

// newWorkspace makes a workspace in temporary folders.
func newWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	s := &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	w, err := s.Create("Learn Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// approve makes a Go plan with one module holding the given objectives the
// curriculum.
func approve(t *testing.T, w *workspace.Workspace, objectives ...string) *workspace.Curriculum {
	t.Helper()
	if err := w.Propose(plan(objectives...)); err != nil {
		t.Fatal(err)
	}
	c, err := w.ApproveProposal()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func plan(objectives ...string) workspace.Curriculum {
	m := workspace.Module{ID: "basics", Title: "Basics", References: []workspace.Source{{Title: "Go docs", URL: "https://go.dev/doc/"}}}
	for _, id := range objectives {
		m.Objectives = append(m.Objectives, workspace.Objective{ID: id, Name: "Objective " + id, Title: "Prove " + id})
	}
	return workspace.Curriculum{Language: "go", Modules: []workspace.Module{m}}
}

// addTask saves a task for an objective, as if it had passed the gate.
func addTask(t *testing.T, w *workspace.Workspace, objective string) workspace.TaskInfo {
	t.Helper()
	info, err := w.SaveTask(taskSpec(objective), "go", runner.TestNames{Visible: []string{"TestSum"}, Hidden: []string{"TestSumHidden"}})
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// taskSpec is a Go task that passes ValidateSpec.
func taskSpec(objective string) workspace.TaskSpec {
	return workspace.TaskSpec{
		ObjectiveID: objective,
		Title:       "Sum " + objective,
		Statement: workspace.Statement{
			Task:         "Write Sum so that it returns a + b.",
			Signature:    "func Sum(a, b int) int",
			Requirements: []string{"Sum returns a + b."},
			Examples:     []workspace.Example{{Input: "Sum(1, 2)", Output: "3"}, {Input: "Sum(-1, 1)", Output: "0"}},
			Context:      "Totals in your budget app.",
		},
		Starter:     runner.Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return 0 }\n"},
		Reference:   runner.Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a + b }\n"},
		Tests:       runner.Files{"sum_test.go": "package task\n"},
		HiddenTests: runner.Files{"sum_hidden_test.go": "package task\n"},
		Mutants: []runner.Mutant{
			{Name: "Off by one", Files: runner.Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a + b + 1 }\n"}},
			{Name: "Ignores b", Files: runner.Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a }\n"}},
		},
	}
}

// addLesson saves a three-question lesson for an objective.
func addLesson(t *testing.T, w *workspace.Workspace, objective string) workspace.LessonInfo {
	t.Helper()
	info, err := w.SaveLesson(lessonSpec(objective))
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// lessonSpec is a lesson that passes ValidateLesson. The answers are 0, 1
// and 2.
func lessonSpec(objective string) workspace.LessonSpec {
	return workspace.LessonSpec{
		ObjectiveID:   objective,
		Title:         "Adding numbers",
		Summary:       "You'll add two numbers in Go.",
		Body:          "## Adding\n\nUse `+`, as [the spec](https://go.dev/ref/spec#Arithmetic_operators) says:\n\n```go\n# not a heading inside code\nx := 1 + 2\n```\n",
		PrimarySource: workspace.Source{Title: "The Go spec", URL: "https://go.dev/ref/spec"},
		Quiz: []workspace.QuizQuestion{
			{Question: "What is 1 + 2?", Options: []string{"three", "four", "five"}, Answer: 0, Explanation: "One plus two is three."},
			{Question: "What is 2 + 2?", Options: []string{"three", "four", "five"}, Answer: 1, Explanation: "Two plus two is four."},
			{Question: "What is 2 + 3?", Options: []string{"three", "four", "five"}, Answer: 2, Explanation: "Two plus three is five."},
		},
	}
}

func record(t *testing.T, w *workspace.Workspace, attempts ...workspace.Attempt) {
	t.Helper()
	for _, a := range attempts {
		if err := w.RecordAttempt(a); err != nil {
			t.Fatal(err)
		}
	}
}

// wantProblems fails unless err mentions every one of want, or, with no
// want, unless err is nil.
func wantProblems(t *testing.T, err error, want ...string) {
	t.Helper()
	if len(want) == 0 {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("no error, want one mentioning %q", want)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error doesn't mention %q:\n%v", w, err)
		}
	}
}
