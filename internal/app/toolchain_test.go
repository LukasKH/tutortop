package app

import (
	"maps"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
)

// TestSetToolchainEnablesALanguage sets up JavaScript the way the tutor would.
func TestSetToolchainEnablesALanguage(t *testing.T) {
	a := sandboxApp(t)
	w, err := a.store.Create("Learn JavaScript", "claude")
	if err != nil {
		t.Fatal(err)
	}
	call := a.caller(t, w, workspace.PlanThread)

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
}
