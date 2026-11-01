package runner

import (
	"context"
	"strings"
	"testing"
)

func report(xml string) string {
	return "some output\n" + junitBegin + "\n" + xml + "\n" + junitEnd + "\n"
}

func TestParseJUnitRunsMergesRepetitions(t *testing.T) {
	pass := `<testsuites><testsuite><testcase classname="a" name="adds"/><testcase classname="test-hidden" name="edge"/></testsuite></testsuites>`
	flaky := `<testsuite><testcase classname="a" name="adds"><failure message="boom">trace</failure></testcase><testcase classname="test-hidden" name="edge"/></testsuite>`
	got, ok := parseJUnitRuns(report(pass)+report(flaky), map[string]bool{"test-hidden": true})
	if !ok || len(got) != 2 {
		t.Fatalf("ok=%v got=%+v", ok, got)
	}
	if got[0].Passed || !strings.Contains(got[0].Message, "boom") {
		t.Errorf("a test that failed in one run must fail: %+v", got[0])
	}
	if !got[1].Passed || !got[1].Hidden {
		t.Errorf("edge should pass and be hidden: %+v", got[1])
	}
}

func TestParseJUnitRunsNeedsAReport(t *testing.T) {
	for name, out := range map[string]string{
		"no markers":   "compile error",
		"empty report": report(""),
		"no tests":     report(`<testsuites></testsuites>`),
	} {
		if _, ok := parseJUnitRuns(out, nil); ok {
			t.Errorf("%s: want build failure", name)
		}
	}
}

func TestParseJUnitRunsNestedSuites(t *testing.T) {
	xml := `<testsuites><testsuite name="outer"><testsuite name="inner"><testcase name="deep" file="/work/test_hidden.js"/></testsuite></testsuite></testsuites>`
	got, ok := parseJUnitRuns(report(xml), map[string]bool{"test_hidden": true})
	if !ok || len(got) != 1 || !got[0].Hidden {
		t.Fatalf("ok=%v got=%+v", ok, got)
	}
}

func TestToolchainSpecReportsEveryProblem(t *testing.T) {
	_, err := ToolchainSpec{Language: "go", TestPattern: "test_\\w+"}.Toolchain()
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"built in", "name is required", "start with FROM", "test_command", "test_files", "group"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestStripJUnitKeepsOutput(t *testing.T) {
	got := stripJUnit("one\n" + report("<x/>") + "two\n" + report("<y/>"))
	if strings.Contains(got, "<x/>") || !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Fatalf("got %q", got)
	}
}

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
