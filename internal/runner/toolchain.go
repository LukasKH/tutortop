package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/LukasKH/tutortop/internal/text"
)

// Toolchain knows how to run one language's tests and read the results.
type Toolchain struct {
	Language string // id used in the curriculum, e.g. "go"
	Name     string // shown to people, e.g. "Go"
	// EditorLanguage is the Monaco language id for syntax highlighting.
	EditorLanguage string
	// Image is the container image checks run in, built locally from
	// Dockerfile the first time it is needed. Bump the tag when the
	// Dockerfile changes.
	Image      string
	Dockerfile string
	// Script returns the shell command that runs the tests in the task
	// directory, repeating each test `repeat` times, and prints everything
	// Parse needs.
	Script func(repeat int) string
	// ExtraFiles are written next to the task files before running.
	ExtraFiles map[string]string
	// Parse turns the script's output into per-test results.
	// hiddenNames holds names of tests defined in hidden check files.
	Parse func(stdout string, hiddenNames map[string]bool) ([]TestResult, bool)
	// TestNames extracts test names from a test file, so results can be
	// labelled visible or hidden.
	TestNames func(filename, src string) []string
	// LanguageServer is the command that runs a language server over stdio
	// inside the image, for completions and diagnostics in the editor.
	// Empty means the editor gets syntax highlighting only.
	LanguageServer []string

	testFile *regexp.Regexp // matches a test file's base name
	testFunc *regexp.Regexp // matches one test function in a test file
}

// IsTestFile reports whether a file name is one of this language's test files.
func (tc Toolchain) IsTestFile(name string) bool {
	return tc.testFile.MatchString(path.Base(name))
}

// CountTests estimates how many tests a file defines, for display only.
func (tc Toolchain) CountTests(src string) int {
	return len(tc.testFunc.FindAllString(src, -1))
}

var builtins = map[string]Toolchain{
	"go":     goToolchain,
	"python": pythonToolchain,
}

// Builtin returns a toolchain the app ships with: "go" or "python". Other
// languages are set up per workspace with a ToolchainSpec.
func Builtin(language string) (Toolchain, bool) {
	tc, ok := builtins[language]
	return tc, ok
}

// ---- Go ----

var goToolchain = Toolchain{
	Language:       "go",
	Name:           "Go",
	EditorLanguage: "go",
	Image:          "tutortop-go:2",
	// Runs as an unprivileged user, with the standard library, vet and
	// test2json already compiled so a check only builds the task's package.
	// (Pattern from Exercism's go-test-runner image.) gopls powers the editor.
	Dockerfile: `FROM golang:1.27-alpine
RUN adduser -D -u 1000 runner && mkdir /work && chown runner /work
USER runner
ENV CGO_ENABLED=0 GOFLAGS=-mod=mod GOTOOLCHAIN=local GOPATH=/home/runner/go
RUN GOFLAGS= go install golang.org/x/tools/gopls@latest
RUN mkdir /tmp/warm && cd /tmp/warm \
 && printf 'module warm\n\ngo 1.27\n' > go.mod \
 && printf 'package warm\n\nimport "testing"\n\nfunc TestWarm(t *testing.T) {}\n' > warm_test.go \
 && go test -json ./... > /dev/null && go build std && rm -rf /tmp/warm
WORKDIR /work
`,
	ExtraFiles: map[string]string{"go.mod": "module task\n\ngo 1.27\n"},
	Script: func(repeat int) string {
		flags := "-count=1"
		if repeat > 1 {
			flags = fmt.Sprintf("-count=%d -shuffle=on", repeat)
		}
		return "CGO_ENABLED=0 GOFLAGS=-mod=mod GOTOOLCHAIN=local go test -json " + flags + " -timeout=20s ./... 2>&1"
	},
	Parse: parseGoTestJSON,
	TestNames: func(_, src string) []string {
		return matchAll(goTestFunc, src)
	},
	LanguageServer: []string{"/home/runner/go/bin/gopls"},
	testFile:       regexp.MustCompile(`_test\.go$`),
	testFunc:       goTestFunc,
}

var goTestFunc = regexp.MustCompile(`(?m)^func (Test\w*)\(\s*\w+\s+\*testing\.T\s*\)`)

type goTestEvent struct {
	Action string
	Test   string
	Output string
}

// parseGoTestJSON reads `go test -json` output. Build failures produce no test
// events, so ok=false tells the caller the code didn't compile.
func parseGoTestJSON(stdout string, hidden map[string]bool) ([]TestResult, bool) {
	var order []string
	results := map[string]*TestResult{}
	output := map[string]*strings.Builder{}
	failed := map[string]bool{}
	sawEvents, packageFailed := false, false

	for line := range strings.Lines(stdout) {
		var ev goTestEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Test == "" {
			packageFailed = packageFailed || ev.Action == "fail"
			continue
		}
		sawEvents = true
		// Subtests ("TestX/case") are reported under their parent's output.
		top, _, _ := strings.Cut(ev.Test, "/")
		if _, ok := results[top]; !ok {
			results[top] = &TestResult{Name: top, Hidden: hidden[top]}
			output[top] = &strings.Builder{}
			order = append(order, top)
		}
		switch ev.Action {
		case "output":
			if !strings.HasPrefix(strings.TrimSpace(ev.Output), "=== ") {
				output[top].WriteString(ev.Output)
			}
		case "pass":
			// With -count>1 a test reports once per repetition; one failed
			// repetition fails the test.
			if ev.Test == top && !failed[top] {
				results[top].Passed = true
			}
		case "fail":
			if ev.Test == top {
				results[top].Passed = false
				failed[top] = true
			}
		}
	}
	out := make([]TestResult, 0, len(order))
	allPassed := true
	for _, name := range order {
		r := results[name]
		if !r.Passed {
			r.Message = trimMessage(output[name].String())
			allPassed = false
		}
		out = append(out, *r)
	}
	// Every test passed but the test program still failed, e.g. a failing
	// TestMain or an os.Exit after the tests. That isn't a pass.
	if sawEvents && allPassed && packageFailed {
		out = append(out, TestResult{Name: "(test program)", Message: "The test program failed outside any single test. See the output."})
	}
	return out, sawEvents
}

// ---- Python ----

var pythonToolchain = Toolchain{
	Language:       "python",
	Name:           "Python",
	EditorLanguage: "python",
	Image:          "tutortop-python:4",
	// basedpyright powers the editor (it ships its own Node.js).
	Dockerfile: `FROM python:3.14-slim
RUN pip install --no-cache-dir 'pytest==8.*' 'pytest-timeout==2.*' 'pytest-repeat==0.9.*' 'basedpyright==1.*' \
 && useradd -m -u 1000 runner && mkdir /work && chown runner /work
USER runner
ENV PYTHONDONTWRITEBYTECODE=1
WORKDIR /work
`,
	// pytest repeats tests itself (pytest-repeat), so the report is written once.
	Script: func(repeat int) string {
		count := ""
		if repeat > 1 {
			count = fmt.Sprintf(" --count=%d", repeat)
		}
		return junitScript("python -m pytest -q -p no:cacheprovider --timeout=20"+count+" --junitxml=/tmp/junit.xml", 1)
	},
	Parse: parseJUnitRuns,
	TestNames: func(filename, src string) []string {
		// pytest reports classname as "<module>" or "<module>.<Class>", so we
		// tag hidden tests by module name instead of by function name.
		return []string{strings.TrimSuffix(path.Base(filename), ".py")}
	},
	LanguageServer: []string{"basedpyright-langserver", "--stdio"},
	testFile:       regexp.MustCompile(`^test_.*\.py$`),
	testFunc:       regexp.MustCompile(`(?m)^\s*def (test\w*)\(`),
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	File      string        `xml:"file,attr"`
	Failure   *junitProblem `xml:"failure"`
	Error     *junitProblem `xml:"error"`
	Skipped   *junitProblem `xml:"skipped"`
}
type junitProblem struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// ---- helpers ----

func matchAll(re *regexp.Regexp, src string) []string {
	var names []string
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		names = append(names, m[1])
	}
	return names
}

func trimMessage(s string) string {
	return text.Truncate(strings.TrimSpace(s), 2000)
}

// stripJUnit removes the machine-readable reports from output shown to people.
func stripJUnit(s string) string {
	var b strings.Builder
	for {
		before, rest, found := strings.Cut(s, junitBegin)
		b.WriteString(before)
		if !found {
			break
		}
		_, s, _ = strings.Cut(rest, junitEnd)
	}
	return strings.TrimSpace(b.String())
}

// humanGoOutput turns `go test -json` lines back into readable text.
func humanGoOutput(s string) string {
	var b bytes.Buffer
	for line := range strings.Lines(s) {
		var ev goTestEvent
		if json.Unmarshal([]byte(line), &ev) == nil && ev.Action != "" {
			if ev.Action == "output" || ev.Action == "build-output" {
				b.WriteString(ev.Output)
			}
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}
