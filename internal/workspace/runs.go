package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LukasKH/tutortop/internal/runner"
)

// A task's latest test run is kept so its tutor knows how the learner's
// last check went, even after a restart. It's in the private folder, not in
// .tutortop/: a submit's result names the hidden tests and quotes their
// failures and output, and .tutortop/ is committed to the workspace's git
// repo, where the tutor can read it. It goes when the workspace is deleted.

// SaveLastRun keeps a task's latest test run, replacing the one before.
func (w *Workspace) SaveLastRun(taskID string, res runner.Result) error {
	if !validName(taskID) {
		return fmt.Errorf("invalid task id %q", taskID)
	}
	p := w.lastRunPath(taskID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return writeJSON(p, res)
}

// LastRun returns a task's latest test run, if it was run.
func (w *Workspace) LastRun(taskID string) (runner.Result, bool) {
	if !validName(taskID) {
		return runner.Result{}, false
	}
	res, err := readJSON[runner.Result](w.lastRunPath(taskID))
	return res, err == nil
}

func (w *Workspace) lastRunPath(taskID string) string {
	return w.private("tasks", taskID, "last_run.json")
}
