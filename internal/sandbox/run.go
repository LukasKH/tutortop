package sandbox

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/text"
)

// Job is one check run: the learner's (or reference/mutant) solution plus tests.
type Job struct {
	Toolchain   Toolchain
	Solution    Files
	Tests       Files // visible tests
	HiddenTests Files // only included when running a full submission
	// Repeat runs every test this many times (shuffled where the language
	// supports it); a test passes only if every repetition passes.
	Repeat  int
	Timeout time.Duration
	// Names are the task's tests as the quality gate saw them run. They label
	// results hidden, and a test missing from a run shows as not run. Empty
	// while the gate itself runs.
	Names TestNames
	// Cache names the runs that share the toolchain's cache folder, such as
	// Go's build cache, so they don't redo each other's work: a workspace's
	// id. Empty for none, as in the quality gate, which checks a new task
	// from a clean slate.
	Cache string
}

// Files maps relative paths to file contents.
type Files map[string]string

// TestNames are a task's tests, named as its test framework reports them.
type TestNames struct {
	Visible []string `yaml:"visible" json:"visible"`
	Hidden  []string `yaml:"hidden" json:"hidden"`
}

type Result struct {
	Status     Status       `json:"status"`
	Tests      []TestResult `json:"tests"`
	Output     string       `json:"output"`
	DurationMs int64        `json:"durationMs"`
}

func (r Result) Counts() (passed, total int) {
	for _, t := range r.Tests {
		if t.Passed {
			passed++
		}
	}
	return passed, len(r.Tests)
}

// Status is how a check run ended.
type Status string

const (
	StatusPassed Status = "passed" // compiled and every test passed
	StatusFailed Status = "failed" // compiled, at least one test failed
	StatusError  Status = "error"  // didn't compile, timed out, or the sandbox broke
)

type TestResult struct {
	Name    string `json:"name"`
	Hidden  bool   `json:"hidden"`
	Passed  bool   `json:"passed"`
	NotRun  bool   `json:"notRun,omitempty"` // the run stopped before reaching it
	Message string `json:"message,omitempty"`
}

// Run executes the job and returns per-test results. An error is returned only
// for infrastructure problems; code that fails to build, hangs or floods the
// output is a Result with StatusError.
func (r *Runner) Run(ctx context.Context, job Job) (Result, error) {
	tc := job.Toolchain
	if err := r.Prepare(ctx, tc); err != nil {
		return Result{}, err
	}
	timeout := cmp.Or(job.Timeout, 45*time.Second) // the test command itself stops after 20s

	files := Files{}
	maps.Copy(files, tc.ExtraFiles)
	for _, set := range []Files{job.Solution, job.Tests, job.HiddenTests} {
		for name, content := range set {
			if !filepath.IsLocal(name) {
				return Result{}, fmt.Errorf("file path %q must be relative and stay inside the task", name)
			}
			files[name] = content
		}
	}
	hidden := map[string]bool{}
	for _, n := range job.Names.Hidden {
		hidden[n] = true
	}

	runCtx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	runCtx, stopTimer := context.WithTimeoutCause(runCtx, timeout, errTimeout)
	defer stopTimer()
	out := &cappedBuffer{max: maxOutput, stop: func() { stop(errTooMuchOutput) }}

	start := time.Now()
	runErr := r.exec(runCtx, tc, tc.script(max(job.Repeat, 1)), files, job.Cache, out)
	res := Result{DurationMs: time.Since(start).Milliseconds()}
	stdout := out.String()

	switch context.Cause(runCtx) {
	case errTimeout:
		res.Status = StatusError
		res.Output = fmt.Sprintf("Timed out after %s. Look for an infinite loop or a blocked channel.", timeout)
		return res, nil
	case errTooMuchOutput:
		res.Status = StatusError
		res.Output = "Your code printed more than 4 MB, so the run was stopped. Look for printing inside a loop."
		return res, nil
	}
	if runErr != nil && stdout == "" {
		return Result{}, runErr
	}

	tests, ok := tc.parse(stdout, hidden)
	res.Output = text.Tail(tc.readable(stdout), 8000)
	if !ok || len(tests) == 0 {
		res.Status = StatusError
		return res, nil
	}
	res.Tests = append(tests, notRun(job, tests)...)
	res.Status = StatusPassed
	for _, t := range res.Tests {
		if !t.Passed {
			res.Status = StatusFailed
			break
		}
	}
	return res, nil
}

const maxOutput = 4 << 20 // bytes of test output kept before the run is stopped

var (
	errTimeout       = errors.New("timed out")
	errTooMuchOutput = errors.New("too much output")
)

// notRun lists the task's tests that never reported a result. A crash can
// end a run early: a panic stops `go test` at the crashing test, so later
// tests (often the visible ones) would silently vanish from the results.
func notRun(job Job, got []TestResult) []TestResult {
	seen := map[string]bool{}
	for _, t := range got {
		seen[t.Name] = true
	}
	var missing []TestResult
	add := func(names []string, hidden bool) {
		for _, name := range names {
			if !seen[name] {
				seen[name] = true
				missing = append(missing, TestResult{Name: name, Hidden: hidden, NotRun: true, Message: "Didn't run: the tests stopped early, most likely because another test crashed. See the output."})
			}
		}
	}
	add(job.Names.Visible, false)
	if len(job.HiddenTests) > 0 {
		add(job.Names.Hidden, true)
	}
	return missing
}

// cappedBuffer collects output up to max bytes and calls stop when more
// arrives, so a print loop can't exhaust the app's memory. The mutex matters:
// with WaitDelay set, cmd.Wait can return before the copying goroutine ends.
type cappedBuffer struct {
	max  int
	stop func()

	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.buf.Len()+len(p) > c.max {
		c.stop() // the output is discarded once this happens
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}
