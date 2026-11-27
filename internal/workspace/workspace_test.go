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
		{"SaveChatImage", func(bad string) error { _, err := w.SaveChatImage(bad, []byte("GIF89a")); return err }},
		{"ChatImage thread", func(bad string) error { _, err := w.ChatImage(bad, "a.png"); return err }},
		{"ChatImage name", func(bad string) error { _, err := w.ChatImage(workspace.PlanThread.String(), bad); return err }},
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

// The toolchains in use are those of the workspaces not yet completed. Folders
// that aren't workspaces don't count, but one that can't be read fails the
// call, as its toolchains would look unused and lose their images.
func TestToolchainsInUse(t *testing.T) {
	s := &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	spec := func(language string) sandbox.ToolchainSpec {
		return sandbox.ToolchainSpec{Language: language, Name: language, Dockerfile: "FROM docker.io/library/alpine:3", TestCommand: "true", TestFiles: "test_*"}
	}
	learning, err := s.Create("Learn R", "claude")
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Create("Learn Rust", "claude")
	if err != nil {
		t.Fatal(err)
	}
	for w, language := range map[*workspace.Workspace]string{learning: "r", done: "rust"} {
		if err := w.SaveToolchain(spec(language)); err != nil {
			t.Fatal(err)
		}
	}
	approve(t, learning, "a")
	approve(t, done, "a")
	record(t, done, workspace.Attempt{TaskID: addTask(t, done, "a").ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed})
	for _, dir := range []string{"notes", "My Notes"} {
		if err := os.Mkdir(filepath.Join(s.Root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	used, err := s.ToolchainsInUse()
	if err != nil {
		t.Fatal(err)
	}
	if len(used) != 1 || used[0].Language != "r" {
		t.Errorf("toolchains in use %+v, want only r's", used)
	}

	if err := os.WriteFile(filepath.Join(learning.Dir, ".tutortop", "workspace.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ToolchainsInUse(); err == nil {
		t.Error("no error with a workspace that can't be read")
	}
}

// The learner's own edits of their mission and notes are saved trimmed, and
// saving the same text again isn't a change.
func TestSaveDocs(t *testing.T) {
	w := newWorkspace(t)
	for _, doc := range []struct {
		file string
		save func(string) (bool, error)
	}{
		{"MISSION.md", w.SaveMission},
		{"NOTES.md", w.SaveNotes},
	} {
		if changed, err := doc.save("  - Plain words first.\n\n"); err != nil || !changed {
			t.Fatalf("%s: first save changed %v, err %v; want a change", doc.file, changed, err)
		}
		if got := w.ReadDoc(doc.file); got != "- Plain words first.\n" {
			t.Errorf("%s holds %q", doc.file, got)
		}
		if changed, err := doc.save("- Plain words first."); err != nil || changed {
			t.Errorf("%s: saving the same text changed %v, err %v; want no change", doc.file, changed, err)
		}
	}
}

// What the learner already knows comes from the mission's own section, so
// feedback can ask for comparisons to it by name.
func TestPriorKnowledge(t *testing.T) {
	for _, tc := range []struct {
		name, mission, want string
	}{
		{"section", "# Mission\n\n## Why\n\nTools.\n\n## You already know\n\nPython.\n\n## Success looks like\n\n- Ship it\n", "Python"},
		{"list item, any case", "## you already know\n- Python and SQL\n", "Python and SQL"},
		{"no section", "# Mission\n\n## Why\n\nYou already know nothing.\n", ""},
		{"empty section", "## You already know\n\n## Why\n\nTools.\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorkspace(t)
			if _, err := w.SaveMission(tc.mission); err != nil {
				t.Fatal(err)
			}
			if got := w.PriorKnowledge(); got != tc.want {
				t.Errorf("PriorKnowledge() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Summary.Next follows plan order and falls back to the next unpassed
// objective's lesson or name when no unsolved task is written yet.
func TestListSummaryNextFollowsPlanOrder(t *testing.T) {
	w := newWorkspace(t)
	s := &workspace.Store{Root: filepath.Dir(w.Dir), PrivateRoot: filepath.Dir(w.PrivateDir)}
	next := func() string {
		t.Helper()
		list, err := s.List()
		if err != nil || len(list) != 1 {
			t.Fatalf("List() = %+v, %v", list, err)
		}
		return list[0].Next
	}

	if got := next(); got != "" {
		t.Fatalf("Next before plan = %q, want empty", got)
	}
	if err := w.Propose(workspace.Curriculum{
		Language: "go",
		Modules: []workspace.Module{{
			ID:         "m1",
			Title:      "Basics",
			References: []workspace.Source{{Title: "Go", URL: "https://go.dev/"}},
			Objectives: []workspace.Objective{
				{ID: "a", Name: "Variables", Title: "Declare variables"},
				{ID: "b", Name: "Borrowing", Title: "Borrow values"},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}
	if got := next(); got != "Variables" {
		t.Errorf("Next with nothing written = %q, want Variables", got)
	}

	// Pass objective b while objective a only has a lesson (no task yet).
	la := addLesson(t, w, "a")
	tb := addTask(t, w, "b")
	record(t, w, workspace.Attempt{TaskID: tb.ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed})
	if got := next(); got != la.Title {
		t.Errorf("Next with only lesson on unpassed first objective = %q, want %q", got, la.Title)
	}

	// Once objective a gets a task, Next shows the task's title; once passed, "".
	ta := addTask(t, w, "a")
	if got := next(); got != ta.Title {
		t.Errorf("Next with task on first objective = %q, want %q", got, ta.Title)
	}
	record(t, w, workspace.Attempt{TaskID: ta.ID, Kind: workspace.AttemptSubmit, Status: sandbox.StatusPassed})
	if got := next(); got != "" {
		t.Errorf("Next with all objectives passed = %q, want empty", got)
	}
}
