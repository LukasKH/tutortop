package workspace_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// A task's last run outlasts a restart, and stays out of the workspace
// folder, as it quotes the hidden tests.
func TestLastRunIsKeptPrivately(t *testing.T) {
	w := newWorkspace(t)
	if _, ok := w.LastRun("0001-wrap"); ok {
		t.Fatal("a task that never ran has a last run")
	}
	res := runner.Result{Status: runner.StatusFailed, Tests: []runner.TestResult{
		{Name: "TestVisible", Passed: true},
		{Name: "TestHiddenEdge", Hidden: true, Message: "secret expectation"},
	}}
	if err := w.SaveLastRun("0001-wrap", res); err != nil {
		t.Fatal(err)
	}

	s := &workspace.Store{Root: filepath.Dir(w.Dir), PrivateRoot: filepath.Dir(w.PrivateDir)}
	reopened, err := s.Open(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.LastRun("0001-wrap"); !ok || !reflect.DeepEqual(got, res) {
		t.Fatalf("after reopening: %+v, %v", got, ok)
	}
	_ = filepath.WalkDir(w.Dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), "secret expectation") {
				t.Errorf("%s quotes a hidden test", p)
			}
		}
		return nil
	})
}
