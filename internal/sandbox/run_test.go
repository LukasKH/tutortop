package sandbox

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// These run the Go toolchain's real `go test -json` in the sandbox, so they
// pin how its output is read as the Go release changes.
func TestRunGo(t *testing.T) {
	const sum = "package task\n\nfunc Sum(a, b int) int { return a + b }\n"
	cases := []struct {
		name        string
		solution    string
		tests       string
		hiddenTests string // added as a hidden test file when set
		names       TestNames
		status      Status
		want        []TestResult // Message is matched as a substring
	}{
		{
			name:     "subtests fold into their parent, and a failing one fails it",
			solution: sum,
			tests: test(`func TestSum(t *testing.T) {
	t.Run("ok", func(t *testing.T) {})
	t.Run("bad", func(t *testing.T) { t.Errorf("Sum(2, 2) = %d, want 5", Sum(2, 2)) })
}
func TestOther(t *testing.T) {}`),
			status: StatusFailed,
			want:   []TestResult{{Name: "TestSum", Message: "Sum(2, 2) = 4, want 5"}, {Name: "TestOther", Passed: true}},
		},
		{
			// A panic ends the test binary, so tests after it never report.
			name:     "tests after a panic show as not run",
			solution: sum,
			tests: test(`func TestCrash(t *testing.T) { var m map[string]int; m["x"] = 1 }
func TestLater(t *testing.T) {}`),
			names:  TestNames{Visible: []string{"TestCrash", "TestLater"}, Hidden: []string{"TestEdge"}},
			status: StatusFailed,
			// TestEdge is hidden and the job has no hidden tests: a Run, not a Submit.
			want: []TestResult{{Name: "TestCrash", Message: "assignment to entry in nil map"}, {Name: "TestLater", NotRun: true, Message: "Didn't run"}},
		},
		{
			name:        "hidden tests are labelled",
			solution:    sum,
			tests:       test(`func TestSum(t *testing.T) {}`),
			hiddenTests: test(`func TestEdge(t *testing.T) { t.Fatal("edge") }`),
			names:       TestNames{Visible: []string{"TestSum"}, Hidden: []string{"TestEdge", "TestGone"}},
			status:      StatusFailed,
			// Go runs test files in name order: edge_test.go first.
			want: []TestResult{{Name: "TestEdge", Hidden: true, Message: "edge"}, {Name: "TestSum", Passed: true}, {Name: "TestGone", Hidden: true, NotRun: true}},
		},
		{
			name:     "code that doesn't build",
			solution: "package task\n\nfunc Sum(a, b int) int { return a + }\n",
			tests:    test(`func TestSum(t *testing.T) {}`),
			status:   StatusError,
		},
		{
			name:     "a test program that fails outside any test",
			solution: sum,
			tests: test(`func TestMain(m *testing.M) { m.Run(); os.Exit(3) }
func TestSum(t *testing.T) {}`, "os"),
			status: StatusFailed,
			want:   []TestResult{{Name: "TestSum", Passed: true}, {Name: "(test program)", Message: "outside any single test"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			job := Job{Toolchain: builtins["go"], Solution: Files{"sum.go": c.solution}, Tests: Files{"sum_test.go": c.tests}, Names: c.names}
			if c.hiddenTests != "" {
				job.HiddenTests = Files{"edge_test.go": c.hiddenTests}
			}
			res := runJob(t, job)
			if res.Status != c.status {
				t.Fatalf("status %q, want %q; output:\n%s", res.Status, c.status, res.Output)
			}
			if len(res.Tests) != len(c.want) {
				t.Fatalf("tests %+v, want %+v", res.Tests, c.want)
			}
			for i, want := range c.want {
				got := res.Tests[i]
				if got.Name != want.Name || got.Passed != want.Passed || got.Hidden != want.Hidden || got.NotRun != want.NotRun || !strings.Contains(got.Message, want.Message) {
					t.Errorf("test %d is %+v, want %+v", i, got, want)
				}
			}
			if c.status == StatusError && !strings.Contains(res.Output, "sum.go") {
				t.Errorf("the build error isn't in the output:\n%s", res.Output)
			}
			if strings.Contains(res.Output, `"Action"`) {
				t.Errorf("output shows raw test2json events:\n%s", res.Output)
			}
		})
	}
}

// Code that runs too long or prints too much is stopped and ends as an error.
// Both tests end on their own, so a missed stop shows as a pass.
func TestRunStopsRunawayCode(t *testing.T) {
	cases := []struct {
		name    string
		test    string
		timeout time.Duration
	}{
		{"a run past its timeout", test(`func TestSum(t *testing.T) { time.Sleep(15 * time.Second) }`, "time"), 5 * time.Second},
		{"output past the cap", test(`func TestSum(t *testing.T) { fmt.Print(strings.Repeat("x", 5<<20)) }`, "fmt", "strings"), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			job := Job{Toolchain: builtins["go"], Solution: Files{"sum.go": "package task\n"}, Tests: Files{"sum_test.go": c.test}, Timeout: c.timeout}
			if res := runJob(t, job); res.Status != StatusError || len(res.Tests) != 0 {
				t.Errorf("status %q with tests %+v, want an error and no tests", res.Status, res.Tests)
			}
		})
	}
}

func TestRunRejectsPathsOutsideTheTask(t *testing.T) {
	job := Job{Toolchain: builtins["go"], Solution: Files{"../sum.go": ""}}
	if _, err := testRunner(t).Run(t.Context(), job); err == nil {
		t.Fatal("ran a job with a file outside the task folder")
	}
}

// runJob runs a job in the sandbox.
func runJob(t *testing.T, job Job) Result {
	t.Helper()
	res, err := testRunner(t).Run(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// sharedRunner is the tests' runner, so they build each image once.
var sharedRunner = New()

// testRunner returns the tests' runner. The sandbox tests need Docker or
// Podman, so they fail rather than skip without one.
func testRunner(t *testing.T) *Runner {
	t.Helper()
	if err := sharedRunner.Available(t.Context()); err != nil {
		t.Fatalf("the sandbox tests need Docker or Podman: %v", err)
	}
	return sharedRunner
}

// test is a Go test file in package task with the given body.
func test(body string, imports ...string) string {
	var b strings.Builder
	b.WriteString("package task\n\nimport \"testing\"\n")
	for _, imp := range imports {
		fmt.Fprintf(&b, "import %q\n", imp)
	}
	return b.String() + "\n" + body + "\n"
}
