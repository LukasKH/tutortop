package workspace_test

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/LukasKH/tutortop/internal/sandbox"
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
}

// Ids, task ids, lesson ids, thread names and task file names become path
// elements, so none may step outside its folder.
func TestNamesStayInsideTheWorkspace(t *testing.T) {
	w := newWorkspace(t)
	approve(t, w, "a")
	s := &workspace.Store{Root: filepath.Dir(w.Dir), PrivateRoot: filepath.Dir(w.PrivateDir)}
	entries := []struct {
		name string
		call func(bad string) error
	}{
		{"Open", func(bad string) error { _, err := s.Open(bad); return err }},
		{"Task", func(bad string) error { _, err := w.Task(bad); return err }},
		{"Job", func(bad string) error { _, err := w.Job(bad, true); return err }},
		{"SaveWork", func(bad string) error { return w.SaveWork(bad, sandbox.Files{"sum.go": ""}) }},
		{"ResetWork", func(bad string) error { return w.ResetWork(bad) }},
		{"SaveLastRun", func(bad string) error { return w.SaveLastRun(bad, sandbox.Result{}) }},
		{"Lesson", func(bad string) error { _, err := w.Lesson(bad); return err }},
		{"Chat", func(bad string) error { _, err := w.Chat(bad); return err }},
		{"AppendChat", func(bad string) error {
			return w.AppendChat(workspace.ChatItem{Thread: bad, Role: workspace.RoleUser})
		}},
		{"ParseThread", func(bad string) error { _, err := workspace.ParseThread(bad); return err }},
		{"Toolchain", func(bad string) error { _, err := w.Toolchain(bad); return err }},
		// A file name joined under the task's starter folder.
		{"SaveTask", func(bad string) error {
			spec := taskSpec("a")
			spec.Starter = sandbox.Files{filepath.Join("..", "..", bad, "escape.go"): ""}
			_, err := w.SaveTask(spec, "go", sandbox.TestNames{})
			return err
		}},
	}
	for _, e := range entries {
		for _, bad := range []string{"", ".", "..", "../x", `a\b`} {
			if e.call(bad) == nil {
				t.Errorf("%s(%q) succeeded", e.name, bad)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(w.Dir, "tasks", "escape.go")); !os.IsNotExist(err) {
		t.Errorf("SaveTask wrote outside the task folder: %v", err)
	}
	if got := w.ReadDoc("../../etc/passwd"); got != "" {
		t.Errorf("ReadDoc read outside the workspace: %q", got)
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

// Resume ids belong to one agent: switching forgets them, and a late one
// from the old agent is dropped.
func TestResumeIDsFollowTheProvider(t *testing.T) {
	w := newWorkspace(t) // claude
	if err := w.SetResumeID("claude", workspace.PlanThread.String(), "c1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.UseProvider("claude"); got[workspace.PlanThread.String()] != "c1" {
		t.Fatalf("same provider lost its resume ids: %v", got)
	}
	if got, _ := w.UseProvider("codex"); len(got) != 0 {
		t.Fatalf("switching kept resume ids: %v", got)
	}
	if err := w.SetResumeID("claude", workspace.PlanThread.String(), "late"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetResumeID("codex", "0001-task", "x1"); err != nil {
		t.Fatal(err)
	}
	got, _ := w.UseProvider("codex")
	if want := map[string]string{"0001-task": "x1"}; !maps.Equal(got, want) {
		t.Errorf("resume ids %v, want %v", got, want)
	}
}

// A language the workspace set up before the app shipped it keeps the
// toolchain its tasks passed the quality gate with.
func TestOwnToolchainOutranksANewBuiltin(t *testing.T) {
	w := newWorkspace(t)
	// As if python weren't built in when the tutor set it up.
	own := sandbox.ToolchainSpec{Language: "python", Name: "My Python", Dockerfile: "FROM python:3.12", TestCommand: "pytest --junitxml=/tmp/junit.xml", TestFiles: "test_*.py"}
	if err := w.SaveToolchain(own); err != nil {
		t.Fatal(err)
	}
	if tc, err := w.Toolchain("python"); err != nil || tc.Name != "My Python" {
		t.Fatalf("python = %+v, %v; want the workspace's own", tc, err)
	}
}
