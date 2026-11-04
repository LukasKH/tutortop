// Package runner executes task checks in a sandbox and reports per-test results.
package runner

import (
	"archive/tar"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"golang.org/x/sync/singleflight"

	"github.com/LukasKH/tutortop/internal/text"
)

// Status of a check run.
const (
	StatusPassed = "passed" // compiled and every test passed
	StatusFailed = "failed" // compiled, at least one test failed
	StatusError  = "error"  // didn't compile, timed out, or the sandbox broke
)

type TestResult struct {
	Name    string `json:"name"`
	Hidden  bool   `json:"hidden"`
	Passed  bool   `json:"passed"`
	NotRun  bool   `json:"notRun,omitempty"` // the run stopped before reaching it
	Message string `json:"message,omitempty"`
}

type Result struct {
	Status     string       `json:"status"`
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

// Files maps relative paths to file contents.
type Files map[string]string

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
}

// TestNames are a task's tests, named as its test framework reports them.
type TestNames struct {
	Visible []string `yaml:"visible" json:"visible"`
	Hidden  []string `yaml:"hidden" json:"hidden"`
}

// Runner runs jobs either in containers (default) or directly on the host
// (TUTORTOP_RUNNER=local, meant for development only).
type Runner struct {
	Engine string // "docker", "podman" or "local"

	builds   singleflight.Group // callers that need the same image share one build
	mu       sync.Mutex
	prepared map[string]bool
}

// New uses the engine in TUTORTOP_RUNNER, or else Docker if it's installed,
// or else Podman. With neither installed it keeps "docker", and Check says
// how to get one.
func New() *Runner {
	engine := os.Getenv("TUTORTOP_RUNNER")
	if engine == "" {
		engine = "docker"
		if _, err := exec.LookPath("docker"); err != nil {
			if _, err := exec.LookPath("podman"); err == nil {
				engine = "podman"
			}
		}
	}
	return &Runner{Engine: engine, prepared: map[string]bool{}}
}

// Check reports whether the sandbox is usable, with a fix-it hint if not.
func (r *Runner) Check(ctx context.Context) error {
	if r.Engine == "local" {
		return nil
	}
	// Works for both engines, and for Podman's `docker` shim: it fails when
	// Docker's daemon or the Podman machine can't be reached.
	out, err := exec.CommandContext(ctx, r.Engine, "version", "--format", "{{.Server.Version}}").CombinedOutput()
	if err != nil {
		return engineHint(r.Engine, strings.TrimSpace(string(out)), errors.Is(err, exec.ErrNotFound))
	}
	return nil
}

// engineHint turns a failed `<engine> version` into a fix the learner can act on.
func engineHint(engine, output string, notFound bool) error {
	desktop := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	switch {
	case notFound && desktop:
		return errors.New("install Docker Desktop (or Podman Desktop), then restart Tutortop")
	case notFound:
		return errors.New("install docker or podman with your package manager, then restart Tutortop")
	case engine == "podman" && desktop:
		return fmt.Errorf("can't reach Podman. Start it with `podman machine start`: %s", output)
	case strings.Contains(output, "permission denied") && runtime.GOOS == "linux":
		return errors.New("can't reach Docker (permission denied). Add yourself to the docker group: `sudo usermod -aG docker $USER`, then log out and back in")
	case desktop:
		return fmt.Errorf("can't reach Docker. Start Docker Desktop: %s", output)
	case engine == "docker":
		return fmt.Errorf("can't reach Docker. Start it with `sudo systemctl start docker`: %s", output)
	default:
		return fmt.Errorf("can't reach %s: %s", engine, output)
	}
}

// Prepare builds the language image. The first call can take a few minutes.
// A caller can give up waiting; the build carries on for the others.
func (r *Runner) Prepare(ctx context.Context, tc Toolchain) error {
	if r.Engine == "local" {
		return nil
	}
	r.mu.Lock()
	done := r.prepared[tc.Image]
	r.mu.Unlock()
	if done {
		return nil
	}
	build := r.builds.DoChan(tc.Image, func() (any, error) {
		// Its own deadline, so a stuck build doesn't hold up every later caller.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), buildTimeout)
		defer cancel()
		return nil, r.build(ctx, tc)
	})
	select {
	case res := <-build:
		return res.Err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// buildTimeout bounds one image build, including pulling its base image.
const buildTimeout = 20 * time.Minute

func (r *Runner) build(ctx context.Context, tc Toolchain) error {
	if exec.CommandContext(ctx, r.Engine, "image", "inspect", tc.Image).Run() != nil {
		cmd := exec.CommandContext(ctx, r.Engine, "build", "-t", tc.Image, "-")
		cmd.Stdin = strings.NewReader(tc.Dockerfile)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("preparing the %s sandbox: %v\n%s", tc.Name, err, text.Tail(string(out), 1500))
		}
	}
	r.mu.Lock()
	r.prepared[tc.Image] = true
	r.mu.Unlock()
	return nil
}

const maxOutput = 4 << 20 // bytes of test output kept before the run is stopped

var (
	errTimeout       = errors.New("timed out")
	errTooMuchOutput = errors.New("too much output")
)

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
	runErr := r.exec(runCtx, tc, tc.Script(max(job.Repeat, 1)), files, out)
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

	tests, ok := tc.Parse(stdout, hidden)
	res.Output = readable(tc, stdout)
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

func readable(tc Toolchain, stdout string) string {
	s := stripJUnit(stdout)
	if tc.Language == "go" {
		s = humanGoOutput(s)
	}
	return text.Tail(s, 8000)
}

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

// exec unpacks the files into a fresh directory and runs the script there:
// in a throwaway container, or in a temp dir on the host in local mode.
func (r *Runner) exec(ctx context.Context, tc Toolchain, script string, files Files, out *cappedBuffer) error {
	archive, err := tarFiles(files)
	if err != nil {
		return err
	}
	shell := "tar -xf - && " + script
	var cmd *exec.Cmd
	if r.Engine == "local" {
		dir, err := os.MkdirTemp("", "tutortop-run-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		cmd = exec.CommandContext(ctx, "sh", "-c", shell)
		cmd.Dir = dir
	} else {
		// The images run as an unprivileged user with a build cache baked in,
		// so nothing is shared between runs.
		name := "tutortop-" + uuid.New().String()
		args := append([]string{"run", "--rm", "-i", "--name", name}, sandboxFlags("512m")...)
		cmd = exec.CommandContext(ctx, r.Engine, append(args, tc.Image, "sh", "-c", shell)...)
		// Killing the docker client doesn't stop the container, so remove it too.
		cmd.Cancel = func() error {
			_ = exec.Command(r.Engine, "rm", "-f", name).Run()
			return cmd.Process.Kill()
		}
	}
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdin = bytes.NewReader(archive)
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

// sandboxFlags lock a container down: no network, no capabilities, and
// capped memory, CPU and processes.
func sandboxFlags(memory string) []string {
	return []string{
		"--network", "none",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--memory", memory, "--cpus", "1", "--pids-limit", "256",
	}
}

func tarFiles(files Files) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		content := files[name]
		// Owned by the sandbox user (uid 1000), for `docker cp -a`.
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Uid: 1000, Gid: 1000}); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	// Close before taking the bytes: it writes the final padding and the
	// end-of-archive blocks.
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
