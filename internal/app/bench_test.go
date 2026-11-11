package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// The benchmarks time what the learner waits for. `task bench:compare`
// runs them on main and on the working tree and compares the two with
// benchstat; TestLearnerJourney's budgets catch only large slowdowns.

// BenchmarkWorkspace reads the snapshot of a long-used workspace. The
// frontend reads it again after every change, run and published task.
func BenchmarkWorkspace(b *testing.B) {
	a := newTestApp(b)
	w := seasonedWorkspace(b, a, 40, 25)
	for b.Loop() {
		if _, err := a.Workspaces.Workspace(w.ID); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRunTests is a learner's Run and Submit in each built-in language,
// with the image built and the workspace's build cache warm.
func BenchmarkRunTests(b *testing.B) {
	for _, lang := range sandbox.BuiltinLanguages() {
		for _, submit := range []bool{false, true} {
			name := lang + "/run"
			if submit {
				name = lang + "/submit"
			}
			b.Run(name, func(b *testing.B) {
				a := newTestApp(b)
				needSandbox(b, a)
				w := seasonedWorkspace(b, a, 1, 0, lang)
				b.Cleanup(func() { _ = a.runner.RemoveCache(context.Background(), w.ID) })
				tasks, _ := w.Tasks()
				run := func() {
					if _, err := a.Tasks.RunTests(w.ID, tasks[0].ID, words[lang].reference, submit); err != nil {
						b.Fatal(err)
					}
				}
				run() // builds the image and fills the cache
				for b.Loop() {
					run()
				}
			})
		}
	}
}

// BenchmarkCreateTask is a writer publishing a task in each built-in
// language: checking it, the quality gate's three sandbox runs, and saving
// it. The learner waits for this when no task is ready.
func BenchmarkCreateTask(b *testing.B) {
	for _, lang := range sandbox.BuiltinLanguages() {
		b.Run(lang, func(b *testing.B) {
			a := newTestApp(b)
			needSandbox(b, a)
			w := seasonedWorkspace(b, a, 1, 0, lang)
			const objective = "words.o0"
			write := a.caller(b, w, workspace.WriterThread(objective))
			publish := func() {
				if out, isErr := write("create_task", wordsTask(objective, lang)); isErr {
					b.Fatalf("create_task: %s", out)
				}
			}
			publish() // builds the image
			for b.Loop() {
				publish()
			}
		})
	}
}

// seasonedWorkspace is a workspace with an approved plan of n objectives,
// each with a lesson and a task, and attempts runs on each task. The
// objectives' languages cycle through langs, or are Go.
func seasonedWorkspace(tb testing.TB, a *testApp, n, attempts int, langs ...string) *workspace.Workspace {
	tb.Helper()
	if len(langs) == 0 {
		langs = []string{"go"}
	}
	w, err := a.store.Create("Seasoned", "claude")
	if err != nil {
		tb.Fatal(err)
	}
	m := workspace.Module{ID: "words", Title: "Words", References: []workspace.Source{{Title: "Docs", URL: "https://go.dev/doc/"}}}
	for i := range n {
		m.Objectives = append(m.Objectives, workspace.Objective{
			ID:       fmt.Sprintf("words.o%d", i),
			Name:     fmt.Sprintf("Words %d", i),
			Title:    "Split and join words",
			Language: langs[i%len(langs)],
		})
	}
	if err := w.Propose(workspace.Curriculum{Language: "go", Modules: []workspace.Module{m}}); err != nil {
		tb.Fatal(err)
	}
	if _, err := w.ApproveProposal(); err != nil {
		tb.Fatal(err)
	}
	for _, o := range m.Objectives {
		if _, err := w.SaveLesson(wordsLesson(o.ID)); err != nil {
			tb.Fatal(err)
		}
		// Saved as if it had passed the gate, which these names come from.
		task, err := w.SaveTask(wordsTask(o.ID, o.Language), o.Language, words[o.Language].names)
		if err != nil {
			tb.Fatal(err)
		}
		for i := range attempts {
			at := workspace.Attempt{TaskID: task.ID, Kind: workspace.AttemptRun, Status: sandbox.StatusFailed, Total: 1}
			if i == attempts-1 {
				at.Kind, at.Status, at.Passed, at.Total = workspace.AttemptSubmit, sandbox.StatusPassed, 3, 3
			}
			if err := w.RecordAttempt(at); err != nil {
				tb.Fatal(err)
			}
		}
	}
	return w
}
