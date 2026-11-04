package runner

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// These run real `go test -json` on the host (the local engine), so they
// pin how its output is read as the Go release changes.
func TestRunGo(t *testing.T) {
	const sum = "package task\n\nfunc Sum(a, b int) int { return a + b }\n"
	cases := []struct {
		name        string
		solution    string
		tests       string
		hiddenTests string // added as a hidden test file when set
		names       TestNames
		repeat      int
		status      Status
		want        []TestResult // Message is matched as a substring
	}{
		{
			name:     "subtests fold into their parent",
			solution: sum,
			tests: test(`func TestSum(t *testing.T) {
	t.Run("positive", func(t *testing.T) {})
	t.Run("negative", func(t *testing.T) {})
}
func TestOther(t *testing.T) {}`),
			status: StatusPassed,
			want:   []TestResult{{Name: "TestSum", Passed: true}, {Name: "TestOther", Passed: true}},
		},
		{
			name:     "a failing subtest fails its parent with its message",
			solution: sum,
			tests: test(`func TestSum(t *testing.T) {
	t.Run("ok", func(t *testing.T) {})
	t.Run("bad", func(t *testing.T) { t.Errorf("Sum(2, 2) = %d, want 5", Sum(2, 2)) })
}`),
			status: StatusFailed,
			want:   []TestResult{{Name: "TestSum", Message: "Sum(2, 2) = 4, want 5"}},
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
			// The test fails on its second run only: one bad repetition fails it.
			name:     "a failed repetition fails the test",
			solution: sum,
			// One test only: repetitions run shuffled.
			tests: test(`var runs int
func TestFlaky(t *testing.T) { runs++; if runs == 2 { t.Fatal("second run") } }`),
			repeat: 3,
			status: StatusFailed,
			want:   []TestResult{{Name: "TestFlaky", Message: "second run"}},
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
			job := Job{Toolchain: goToolchain, Solution: Files{"sum.go": c.solution}, Tests: Files{"sum_test.go": c.tests}, Names: c.names, Repeat: c.repeat}
			if c.hiddenTests != "" {
				job.HiddenTests = Files{"edge_test.go": c.hiddenTests}
			}
			res := runLocal(t, job)
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

func TestRunRejectsPathsOutsideTheTask(t *testing.T) {
	job := Job{Toolchain: goToolchain, Solution: Files{"../sum.go": ""}}
	if _, err := (&Runner{Engine: "local"}).Run(t.Context(), job); err == nil {
		t.Fatal("ran a job with a file outside the task folder")
	}
}

// runLocal runs a job on the host.
func runLocal(t *testing.T, job Job) Result {
	t.Helper()
	return onHost(t, func(r *Runner) (Result, error) { return r.Run(t.Context(), job) })
}

// onHost runs Go tests on the host with the local engine, and skips without
// the go command.
//
// While containers start and stop on the same machine, as they do in the
// app's tests next to these, a go command started from a test binary is now
// and then killed (SIGKILL, exit status 137) before it prints anything. The
// cause lies outside this package, so such a run is tried again.
func onHost[T any](t *testing.T, run func(*Runner) (T, error)) T {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("needs the go command")
	}
	for try := 1; ; try++ {
		v, err := run(&Runner{Engine: "local"})
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 137 && try < 3 {
			t.Logf("the go command was killed; trying again")
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
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
