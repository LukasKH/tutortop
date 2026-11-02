package runner

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// Tests run against the engine in TUTORTOP_RUNNER, or else Podman or Docker,
// whichever works.
// Use TUTORTOP_RUNNER=local for a quick run without containers.
func testRunner(t *testing.T, language string) *Runner {
	t.Helper()
	r := New()
	if err := r.Check(context.Background()); err != nil {
		t.Skip(err)
	}
	if r.Engine == "local" && language == "python" {
		if exec.Command("python3", "-c", "import pytest").Run() != nil {
			t.Skip("pytest not installed locally")
		}
	}
	return r
}

const goReference = `package solution

import "strings"

// ReverseWords returns the words of s in reverse order, separated by single spaces.
func ReverseWords(s string) string {
	words := strings.Fields(s)
	for i, j := 0, len(words)-1; i < j; i, j = i+1, j-1 {
		words[i], words[j] = words[j], words[i]
	}
	return strings.Join(words, " ")
}
`

const goStarter = `package solution

func ReverseWords(s string) string {
	return ""
}
`

const goVisible = `package solution

import "testing"

func TestBasic(t *testing.T) {
	if got := ReverseWords("hello world"); got != "world hello" {
		t.Fatalf("ReverseWords(%q) = %q, want %q", "hello world", got, "world hello")
	}
}
`

const goHidden = `package solution

import "testing"

func TestExtraSpaces(t *testing.T) {
	if got := ReverseWords("  a   b  "); got != "b a" {
		t.Fatalf("ReverseWords(%q) = %q, want %q", "  a   b  ", got, "b a")
	}
}

func TestEmpty(t *testing.T) {
	if got := ReverseWords(""); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
`

func goInput() GateInput {
	return GateInput{
		Toolchain:   goToolchain,
		Starter:     Files{"solution.go": goStarter},
		Reference:   Files{"solution.go": goReference},
		Tests:       Files{"visible_test.go": goVisible},
		HiddenTests: Files{"hidden_test.go": goHidden},
		Mutants: []Mutant{
			{Name: "split-on-space", Description: "uses strings.Split, keeps empty words", Files: Files{"solution.go": strings.Replace(goReference, "strings.Fields(s)", `strings.Split(s, " ")`, 1)}},
			{Name: "no-reverse", Description: "forgets to reverse", Files: Files{"solution.go": `package solution

import "strings"

func ReverseWords(s string) string { return strings.Join(strings.Fields(s), " ") }
`}},
		},
	}
}

func TestRunReportsHiddenAndVisible(t *testing.T) {
	r := testRunner(t, "go")
	in := goInput()
	res, err := r.Run(context.Background(), Job{Toolchain: goToolchain, Solution: in.Starter, Tests: in.Tests, HiddenTests: in.HiddenTests})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusFailed {
		t.Fatalf("status = %s, output:\n%s", res.Status, res.Output)
	}
	hidden := 0
	for _, tr := range res.Tests {
		if tr.Hidden {
			hidden++
		}
	}
	if len(res.Tests) != 3 || hidden != 2 {
		t.Fatalf("tests = %+v", res.Tests)
	}
}

func TestRunBuildError(t *testing.T) {
	r := testRunner(t, "go")
	res, err := r.Run(context.Background(), Job{Toolchain: goToolchain, Solution: Files{"solution.go": "package solution\nfunc ReverseWords(s string) string { return 1 }\n"}, Tests: Files{"visible_test.go": goVisible}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusError || !strings.Contains(res.Output, "cannot use") {
		t.Fatalf("status = %s, output:\n%s", res.Status, res.Output)
	}
}

func TestGatePassesGoodTask(t *testing.T) {
	r := testRunner(t, "go")
	rep, err := r.Gate(context.Background(), goInput())
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Passed {
		t.Fatal(rep.String())
	}
}

func TestGateCatchesWeakTests(t *testing.T) {
	r := testRunner(t, "go")
	in := goInput()
	in.HiddenTests = Files{"hidden_test.go": "package solution\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n"}
	rep, err := r.Gate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed || !strings.Contains(rep.String(), `mutant "split-on-space" is caught`) {
		t.Fatal(rep.String())
	}
}

func TestPythonRun(t *testing.T) {
	r := testRunner(t, "python")
	res, err := r.Run(context.Background(), Job{
		Toolchain:   pythonToolchain,
		Solution:    Files{"solution.py": "def add(a, b):\n    return a - b\n"},
		Tests:       Files{"test_visible.py": "from solution import add\n\ndef test_add():\n    assert add(1, 2) == 3\n"},
		HiddenTests: Files{"test_hidden.py": "from solution import add\n\ndef test_zero():\n    assert add(0, 0) == 0\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusFailed || len(res.Tests) != 2 {
		t.Fatalf("status=%s tests=%+v\n%s", res.Status, res.Tests, res.Output)
	}
	for _, tr := range res.Tests {
		if tr.Name == "test_zero" && (!tr.Hidden || !tr.Passed) {
			t.Fatalf("test_zero = %+v", tr)
		}
	}
}

func TestRunReportsTestsSkippedByPanic(t *testing.T) {
	r := testRunner(t, "go")
	res, err := r.Run(context.Background(), Job{
		Toolchain:   goToolchain,
		Solution:    Files{"solution.go": "package solution\n\nfunc ReverseWords(s string) string { return s[:1] }\n"},
		Tests:       Files{"visible_test.go": goVisible},
		HiddenTests: Files{"hidden_test.go": goHidden}, // runs first and panics on ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusFailed || len(res.Tests) != 3 {
		t.Fatalf("status=%s tests=%+v", res.Status, res.Tests)
	}
	for _, tr := range res.Tests {
		if tr.Name == "TestBasic" && (tr.Passed || tr.Hidden || !strings.Contains(tr.Message, "Didn't run")) {
			t.Fatalf("TestBasic = %+v", tr)
		}
	}
}

func TestRunPackageFailureIsNotAPass(t *testing.T) {
	r := testRunner(t, "go")
	res, err := r.Run(context.Background(), Job{
		Toolchain: goToolchain,
		Solution:  Files{"solution.go": goReference},
		Tests: Files{"visible_test.go": goVisible + `
func TestMain(m *testing.M) { m.Run(); panic("after the tests") }
`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusFailed {
		t.Fatalf("status=%s tests=%+v", res.Status, res.Tests)
	}
}

func TestRunHangingTestFailsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("takes ~20s")
	}
	r := testRunner(t, "go")
	res, err := r.Run(context.Background(), Job{
		Toolchain: goToolchain,
		Solution:  Files{"solution.go": "package solution\n\nfunc Wait() { select {} }\n"},
		Tests:     Files{"visible_test.go": "package solution\n\nimport \"testing\"\n\nfunc TestFast(t *testing.T) {}\n\nfunc TestWait(t *testing.T) { Wait() }\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusFailed {
		t.Fatalf("status=%s tests=%+v\n%s", res.Status, res.Tests, res.Output)
	}
	for _, tr := range res.Tests {
		if tr.Name == "TestWait" && tr.Passed {
			t.Fatalf("TestWait passed: %+v", res.Tests)
		}
	}
	t.Logf("%+v", res.Tests)
}

func TestRunStopsOnTooMuchOutput(t *testing.T) {
	r := testRunner(t, "go")
	res, err := r.Run(context.Background(), Job{
		Toolchain: goToolchain,
		Solution:  Files{"solution.go": "package solution\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\nfunc Spam() {\n\tfor range 6000 {\n\t\tfmt.Println(strings.Repeat(\"x\", 1000))\n\t}\n}\n"},
		Tests:     Files{"visible_test.go": "package solution\n\nimport \"testing\"\n\nfunc TestSpam(t *testing.T) { Spam() }\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusError || !strings.Contains(res.Output, "more than 4 MB") {
		t.Fatalf("status=%s output=%.200s", res.Status, res.Output)
	}
}

func TestGateCatchesTestsWithSharedState(t *testing.T) {
	r := testRunner(t, "go")
	in := goInput()
	// Passes once, fails when repeated: the kind of test that would be flaky
	// for the learner.
	in.HiddenTests = Files{"hidden_test.go": goHidden + `
var calls int

func TestOnlyOnce(t *testing.T) {
	calls++
	if calls > 1 {
		t.Fatal("depends on state from an earlier run")
	}
}
`}
	rep, err := r.Gate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed || !strings.Contains(rep.String(), "flaky or depend on order") {
		t.Fatal(rep.String())
	}
}
