package workspace_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
)

// Git sets GIT_DIR, GIT_INDEX_FILE and more for its hooks, and tests run in
// a pre-commit hook. The workspace's commits must still land in its own repo.
func TestCommitIgnoresGitEnvironment(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	elsewhere := t.TempDir()
	t.Setenv("GIT_DIR", filepath.Join(elsewhere, "repo.git"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(elsewhere, "index"))

	s := &workspace.Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	w, err := s.Create("Learn Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Dir, "NOTES.md"), []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.Commit("Add notes")

	if entries, _ := os.ReadDir(elsewhere); len(entries) > 0 {
		t.Errorf("git wrote to the inherited paths: %v", entries)
	}
	log := exec.Command("git", "log", "--format=%s")
	log.Dir = w.Dir
	log.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GIT_") })
	out, err := log.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(strings.TrimSpace(string(out))); strings.Join(got, " ") != "Add notes Create workspace" {
		t.Errorf("workspace history %q, want both commits", out)
	}
}
