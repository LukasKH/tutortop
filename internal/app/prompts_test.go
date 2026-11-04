package app

import (
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// Every placeholder in the prompts is filled, for every kind of thread.
func TestInstructionsFillEveryPlaceholder(t *testing.T) {
	a := newTestApp(t)
	w, err := a.store.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	for _, thread := range []workspace.Thread{workspace.PlanThread, workspace.TaskThread("0001-x"), workspace.LessonThread("0001-x"), workspace.WriterThread("go.x")} {
		if text := instructions(w, thread); strings.Contains(text, "{{") {
			i := strings.Index(text, "{{")
			t.Errorf("%s: unfilled placeholder at %q", thread, text[i:min(len(text), i+20)])
		}
	}
}

// The prompts fill the list of built-in languages in ({{builtins}}), but
// each language's conventions in the shared prompt are written by hand, so a
// new built-in language fails this until they're written. (mcp's
// TestWrittenOutLanguagesMatchTheBuiltins checks the tools' struct tags.)
func TestSharedPromptHasEveryBuiltinsConventions(t *testing.T) {
	for _, lang := range sandbox.BuiltinLanguages() {
		tc, _ := sandbox.Builtin(lang)
		if !strings.Contains(sharedPrompt, "\n- **"+tc.Name+" ") {
			t.Errorf("shared.md has no conventions for %s, as a line starting \"- **%s <version>**: \"", lang, tc.Name)
		}
	}
}
