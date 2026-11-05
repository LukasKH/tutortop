package threads

import (
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
)

// Every placeholder in the prompts is filled, for every kind of thread.
func TestInstructionsFillEveryPlaceholder(t *testing.T) {
	w := newTestWorkspace(t)
	for _, thread := range []workspace.Thread{workspace.PlanThread, workspace.TaskThread("0001-x"), workspace.LessonThread("0001-x"), workspace.WriterThread("go.x")} {
		if text := instructions(w, thread); strings.Contains(text, "{{") {
			i := strings.Index(text, "{{")
			t.Errorf("%s: unfilled placeholder at %q", thread, text[i:min(len(text), i+20)])
		}
	}
}
