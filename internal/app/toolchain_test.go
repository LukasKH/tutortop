package app

import (
	"maps"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// TestSetToolchainEnablesALanguage sets up JavaScript the way the tutor would,
// then plans and publishes a task in it through the quality gate.
func TestSetToolchainEnablesALanguage(t *testing.T) {
	a := sandboxApp(t)
	w, err := a.store.Create("Learn JavaScript", "claude")
	if err != nil {
		t.Fatal(err)
	}
	call := toolCaller(t, a, w, workspace.PlanThread)
	plan := map[string]any{
		"language": "javascript",
		"modules": []any{map[string]any{"id": "basics", "title": "Basics", "references": []any{map[string]any{"title": "Docs", "url": "https://go.dev/doc/"}}, "objectives": []any{
			map[string]any{"id": "js.add", "name": "Functions", "title": "Functions"},
		}}},
	}

	// Not set up yet, so the plan is rejected.
	if out, isErr := call("propose_curriculum", plan); !isErr || !strings.Contains(out, "set_toolchain") {
		t.Fatalf("want a rejection pointing at set_toolchain, got %s", out)
	}

	test := func(file, name, call, want string) map[string]string {
		return map[string]string{file: "import { test } from 'node:test';\nimport assert from 'node:assert';\nimport { add } from './add.mjs';\n\n" +
			"test('" + name + "', () => { assert.strictEqual(" + call + ", " + want + "); });\n"}
	}
	toolchain := map[string]any{
		"language": "javascript", "name": "JavaScript", "editorLanguage": "javascript",
		"dockerfile":  "FROM node:24-alpine",
		"testCommand": "node --test --test-reporter=junit --test-reporter-destination=/tmp/junit.xml",
		"testFiles":   "*.test.mjs",
	}
	reference := map[string]string{"add.mjs": "export function add(a, b) { return a + b; }\n"}

	// An example that fails proves nothing, so it's sent back.
	out, isErr := call("set_toolchain", map[string]any{"toolchain": toolchain, "example": map[string]any{
		"solution": map[string]string{"add.mjs": "export function add(a, b) { return a - b; }\n"},
		"tests":    test("add.test.mjs", "adds", "add(1, 2)", "3"),
	}})
	if !isErr || !strings.Contains(out, "FAILED") {
		t.Fatalf("want a failed check, got %s", out)
	}
	if out, isErr := call("set_toolchain", map[string]any{"toolchain": toolchain, "example": map[string]any{
		"solution": reference, "tests": test("add.test.mjs", "adds", "add(1, 2)", "3"),
	}}); isErr {
		t.Fatalf("set_toolchain: %s", out)
	}

	// A language server that doesn't answer doesn't fail the setup; the
	// language is saved without one.
	broken := maps.Clone(toolchain)
	broken["languageServer"] = []string{"false"}
	out, isErr = call("set_toolchain", map[string]any{"toolchain": broken, "example": map[string]any{
		"solution": reference, "tests": test("add.test.mjs", "adds", "add(1, 2)", "3"),
	}})
	if isErr || !strings.Contains(out, "saved without one") {
		t.Fatalf("want a warning, got %s", out)
	}
	if specs := w.ToolchainSpecs(); len(specs) != 1 || specs[0].LanguageServer != nil {
		t.Fatalf("saved = %+v", specs)
	}

	if out, isErr := call("propose_curriculum", plan); isErr {
		t.Fatalf("propose: %s", out)
	}
	if err := a.ApproveProposal(w.ID); err != nil {
		t.Fatal(err)
	}
	out, _ = toolCaller(t, a, w, workspace.WriterThread("js.add"))("create_task", map[string]any{
		"objectiveId": "js.add",
		"title":       "Add numbers",
		"statement": map[string]any{
			"task":         "Write add, which returns the sum of two numbers.",
			"context":      "A calculator needs addition.",
			"signature":    "export function add(a, b)",
			"requirements": []string{"Return a + b."},
			"examples":     []any{map[string]any{"input": "add(1, 2)", "output": "3"}, map[string]any{"input": "add(-1, 1)", "output": "0"}},
		},
		"starter":     map[string]string{"add.mjs": "export function add(a, b) { return 0; }\n"},
		"reference":   reference,
		"tests":       test("add.test.mjs", "adds", "add(1, 2)", "3"),
		"hiddenTests": test("edge.test.mjs", "negatives", "add(-2, -3)", "-5"),
		"mutants": []any{
			map[string]any{"name": "subtracts", "description": "a - b", "files": map[string]string{"add.mjs": "export function add(a, b) { return a - b; }\n"}},
			map[string]any{"name": "abs", "description": "ignores signs", "files": map[string]string{"add.mjs": "export function add(a, b) { return Math.abs(a) + Math.abs(b); }\n"}},
		},
	})
	created, _ := decodeJSON[createTaskOutput]([]byte(out))
	if !created.Published {
		t.Fatalf("task not published:\n%s", out)
	}

	d, err := a.Task(w.ID, created.TaskID)
	if err != nil || d.EditorLanguage != "javascript" || d.HiddenCount != 1 {
		t.Fatalf("task detail = %+v, %v", d, err)
	}
	res, err := a.RunTests(w.ID, created.TaskID, runner.Files{"add.mjs": reference["add.mjs"]}, true)
	if err != nil || res.Status != runner.StatusPassed || len(res.Tests) != 2 || !res.Tests[0].Hidden && !res.Tests[1].Hidden {
		t.Fatalf("submit = %v %+v", err, res)
	}
}
