package runner

import (
	"context"
	"strings"
	"testing"
)

// nodeSpec uses Node's built-in test runner, which writes JUnit itself, so
// the image needs no network-installed packages.
var nodeSpec = ToolchainSpec{
	Language:       "javascript",
	Name:           "JavaScript",
	EditorLanguage: "javascript",
	Dockerfile:     "FROM node:24-alpine",
	TestCommand:    "node --test --test-reporter=junit --test-reporter-destination=/tmp/junit.xml",
	TestFiles:      `\.test\.mjs$`,
	TestPattern:    `test\(["']([^"']+)["']`,
}

func TestCustomToolchainRun(t *testing.T) {
	r := testRunner(t, "javascript")
	if r.Engine == "local" {
		t.Skip("custom toolchains need a container engine")
	}
	tc, err := nodeSpec.Toolchain()
	if err != nil {
		t.Fatal(err)
	}
	test := func(name, call, want string) string {
		return "import { test } from 'node:test';\nimport assert from 'node:assert';\nimport { add } from './add.mjs';\n\n" +
			"test('" + name + "', () => { assert.strictEqual(" + call + ", " + want + "); });\n"
	}
	res, err := r.Run(context.Background(), Job{
		Toolchain:   tc,
		Solution:    Files{"add.mjs": "export function add(a, b) { return a - b; }\n"},
		Tests:       Files{"add.test.mjs": test("adds", "add(1, 2)", "3")},
		HiddenTests: Files{"zero.test.mjs": test("zero", "add(0, 0)", "0")},
		Repeat:      2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusFailed || len(res.Tests) != 2 {
		t.Fatalf("status=%s tests=%+v\n%s", res.Status, res.Tests, res.Output)
	}
	for _, tr := range res.Tests {
		switch tr.Name {
		case "adds":
			if tr.Passed || tr.Hidden {
				t.Errorf("adds = %+v", tr)
			}
		case "zero":
			if !tr.Passed || !tr.Hidden {
				t.Errorf("zero = %+v", tr)
			}
		}
	}
	if strings.Contains(res.Output, junitBegin) {
		t.Error("output shown to the learner contains the raw report")
	}

	// A syntax error is a build failure, not a failed test.
	res, err = r.Run(context.Background(), Job{Toolchain: tc, Solution: Files{"add.mjs": "export function add(a, b) {"}, Tests: Files{"add.test.mjs": test("adds", "add(1, 2)", "3")}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status == StatusPassed {
		t.Fatalf("status=%s tests=%+v", res.Status, res.Tests)
	}
}
