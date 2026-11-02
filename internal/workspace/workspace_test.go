package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/LukasKH/tutortop/internal/runner"
)

// Two coach tool calls can create tasks at the same time; each must get its
// own number even though every call opens its own Workspace value.
func TestParallelSaveTaskGetsUniqueNumbers(t *testing.T) {
	s := &Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	w, err := s.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Propose(Curriculum{Language: "go", Modules: []Module{{ID: "m", Title: "M", References: []Source{{Title: "Go docs", URL: "https://go.dev/doc/"}}, Objectives: []Objective{{ID: "o", Name: "O", Title: "O"}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			ws, err := s.Open(w.ID)
			if err != nil {
				t.Error(err)
				return
			}
			spec := TaskSpec{ObjectiveID: "o", Title: fmt.Sprintf("Task %d", i), Starter: runner.Files{"a.go": "package a"}}
			if _, err := ws.SaveTask(spec, "go"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	tasks, _ := w.Tasks()
	seen := map[string]bool{}
	for _, task := range tasks {
		n := task.ID[:4]
		if seen[n] {
			t.Fatalf("duplicate task number %s in %v", n, tasks)
		}
		seen[n] = true
	}
	if len(tasks) != 8 {
		t.Fatalf("got %d tasks", len(tasks))
	}
}

// Private files are kept by id, not by workspaces folder: after switching
// folders, a new workspace mustn't pick up an old one's hidden tests.
func TestCreateSkipsIDWithPrivateFiles(t *testing.T) {
	private := t.TempDir()
	old := &Store{Root: t.TempDir(), PrivateRoot: private}
	if _, err := old.Create("Go", "claude"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(private, "go"), 0o755); err != nil {
		t.Fatal(err)
	}

	moved := &Store{Root: t.TempDir(), PrivateRoot: private}
	w, err := moved.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if w.ID != "go-2" {
		t.Errorf("id = %q, want go-2", w.ID)
	}
}
