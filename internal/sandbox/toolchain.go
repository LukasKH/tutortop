package sandbox

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/LukasKH/tutortop/internal/text"
)

// Toolchain knows how to run one language's tests and read the results.
// The built-in ones are below; ToolchainSpec makes the others.
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
	// ExtraFiles are written next to the task files before running.
	ExtraFiles map[string]string
	// LanguageServer is the command that runs a language server over stdio
	// inside the image, for completions and diagnostics in the editor.
	// Empty means the editor gets syntax highlighting only.
	LanguageServer []string

	report reportFormat
	// testCommand runs a junitXML toolchain's tests once, writing the
	// report to /tmp/junit.xml.
	testCommand string
	// repeatFlag, if set, makes testCommand repeat each test itself, with %d
	// for the count. Without it the command is run again for each repeat.
	repeatFlag string
	testFiles  string // glob matching a test file's base name
}

// IsTestFile reports whether a file name is one of this language's test files.
func (tc Toolchain) IsTestFile(name string) bool {
	ok, _ := path.Match(tc.testFiles, path.Base(name))
	return ok
}

// script returns the shell command that runs the tests in the task
// directory, repeating each test `repeat` times, and prints everything parse
// needs.
func (tc Toolchain) script(repeat int) string {
	switch tc.report {
	case goJSON:
		return goScript(repeat)
	case junitXML:
		if tc.repeatFlag == "" || repeat <= 1 {
			return junitScript(tc.testCommand, repeat)
		}
		return junitScript(tc.testCommand+" "+fmt.Sprintf(tc.repeatFlag, repeat), 1)
	}
	return ""
}

// parse turns the script's output into per-test results, labelling those in
// hidden as hidden. ok is false when the code didn't build.
func (tc Toolchain) parse(stdout string, hidden map[string]bool) (tests []TestResult, ok bool) {
	switch tc.report {
	case goJSON:
		return parseGoTestJSON(stdout, hidden)
	case junitXML:
		return parseJUnitRuns(stdout, hidden)
	}
	return nil, false
}

// readable turns the script's output into text for the learner, without
// what only parse needs.
func (tc Toolchain) readable(stdout string) string {
	switch tc.report {
	case goJSON:
		return humanGoOutput(stdout)
	case junitXML:
		return stripJUnit(stdout)
	}
	return stdout
}

// builtins are the languages the app ships with. The app's descriptions and
// prompts for the agent name them through BuiltinLanguages. Struct tags name
// them by hand, and the shared prompt has each one's conventions, so a new
// one is added to both by hand. docs/architecture.md has the steps for adding
// one.
var builtins = map[string]Toolchain{
	"go":     goToolchain,
	"python": pythonToolchain,
}

// Builtin returns a toolchain the app ships with. Other languages are set up
// per workspace with a ToolchainSpec.
func Builtin(language string) (Toolchain, bool) {
	tc, ok := builtins[language]
	return tc, ok
}

// BuiltinLanguages lists the ids of the languages the app ships with, sorted.
func BuiltinLanguages() []string { return slices.Sorted(maps.Keys(builtins)) }

// reportFormat is how a toolchain's test command reports results.
type reportFormat int

const (
	// The zero value is no format, so a toolchain that forgets one parses
	// nothing instead of guessing.
	goJSON   reportFormat = iota + 1 // `go test -json`
	junitXML                         // a JUnit XML report per run (junitScript)
)

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
	ExtraFiles:     map[string]string{"go.mod": "module task\n\ngo 1.27\n"},
	report:         goJSON,
	LanguageServer: []string{"/home/runner/go/bin/gopls"},
	testFiles:      "*_test.go",
}

func goScript(repeat int) string {
	flags := "-count=1"
	if repeat > 1 {
		flags = fmt.Sprintf("-count=%d -shuffle=on", repeat)
	}
	return "CGO_ENABLED=0 GOFLAGS=-mod=mod GOTOOLCHAIN=local go test -json " + flags + " -timeout=20s ./... 2>&1"
}

type goTestEvent struct {
	Action goAction
	Test   string
	Output string
}

// goAction is a `go test -json` event's action, those the app reads.
type goAction string

const (
	goOutput      goAction = "output"
	goBuildOutput goAction = "build-output"
	goPass        goAction = "pass"
	goFail        goAction = "fail"
)

// parseGoTestJSON reads `go test -json` output. Build failures produce no test
// events, so ok=false tells the caller the code didn't compile.
func parseGoTestJSON(stdout string, hidden map[string]bool) ([]TestResult, bool) {
	var order []string
	results := map[string]*TestResult{}
	output := map[string]*strings.Builder{}
	failed := map[string]bool{}
	sawEvents, packageFailed := false, false

	var ev goTestEvent
	for line := range strings.Lines(stdout) {
		ev = goTestEvent{} // or the previous line's fields leak into this one
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Test == "" {
			packageFailed = packageFailed || ev.Action == goFail
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
		case goOutput:
			if !strings.HasPrefix(strings.TrimSpace(ev.Output), "=== ") {
				output[top].WriteString(ev.Output)
			}
		case goPass:
			// With -count>1 a test reports once per repetition; one failed
			// repetition fails the test.
			if ev.Test == top && !failed[top] {
				results[top].Passed = true
			}
		case goFail:
			if ev.Test == top {
				results[top].Passed = false
				failed[top] = true
			}
		case goBuildOutput:
			// Build output has no test; it's shown with the whole output.
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

// humanGoOutput turns `go test -json` lines back into readable text.
func humanGoOutput(s string) string {
	var b bytes.Buffer
	var ev goTestEvent
	for line := range strings.Lines(s) {
		ev = goTestEvent{} // or the previous line's fields leak into this one
		if json.Unmarshal([]byte(line), &ev) == nil && ev.Action != "" {
			if ev.Action == goOutput || ev.Action == goBuildOutput {
				b.WriteString(ev.Output)
			}
			continue
		}
		b.WriteString(line)
	}
	return b.String()
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
	report:      junitXML,
	testCommand: "python -m pytest -q -p no:cacheprovider --timeout=20 --junitxml=/tmp/junit.xml",
	// pytest repeats tests itself (pytest-repeat), so the report is written once.
	repeatFlag:     "--count=%d",
	LanguageServer: []string{"basedpyright-langserver", "--stdio"},
	testFiles:      "test_*.py",
}

// ---- JUnit ----

const (
	junitBegin = "===TUTORTOP-JUNIT-BEGIN==="
	junitEnd   = "===TUTORTOP-JUNIT-END==="
)

// junitScript runs the test command `repeat` times (frameworks have no common
// way to repeat or shuffle tests) and prints each report between markers.
func junitScript(command string, repeat int) string {
	run := "rm -f /tmp/junit.xml; ( " + command + " ) 2>&1; " +
		"echo; echo '" + junitBegin + "'; cat /tmp/junit.xml 2>/dev/null; echo; echo '" + junitEnd + "'"
	return strings.Repeat(run+"; ", max(repeat, 1)-1) + run
}

// junitNode reads both <testsuites> and a lone <testsuite> root, with
// suites nested to any depth.
type junitNode struct {
	XMLName xml.Name
	Suites  []junitNode `xml:"testsuite"`
	Cases   []junitCase `xml:"testcase"`
}

func (n junitNode) cases() []junitCase {
	out := slices.Clone(n.Cases)
	for _, s := range n.Suites {
		out = append(out, s.cases()...)
	}
	return out
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Failure   *junitProblem `xml:"failure"`
	Error     *junitProblem `xml:"error"`
	Skipped   *junitProblem `xml:"skipped"`
}
type junitProblem struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// parseJUnitRuns merges the reports of every repetition: a test passes only
// if it passed in every run. A run without a readable report, or with no
// tests, means the code didn't build.
func parseJUnitRuns(stdout string, hidden map[string]bool) ([]TestResult, bool) {
	var order []string
	results := map[string]*TestResult{}
	runs := 0
	for rest := stdout; ; {
		i := strings.Index(rest, junitBegin)
		if i < 0 {
			break
		}
		rest = rest[i+len(junitBegin):]
		report, after, _ := strings.Cut(rest, junitEnd)
		rest = after
		runs++

		doc, err := decode.XML[junitNode]([]byte(strings.TrimSpace(report)))
		if err != nil {
			return nil, false
		}
		cases := doc.cases()
		if len(cases) == 0 {
			return nil, false
		}
		for _, c := range cases {
			// A collection error (e.g. a syntax error) shows up as an errored
			// case with no name: the code didn't build.
			if c.Name == "" && c.Error != nil {
				return nil, false
			}
			key := c.Classname + "\x00" + c.Name
			r, ok := results[key]
			if !ok {
				r = &TestResult{Name: c.Name, Hidden: hidden[c.Name], Passed: true}
				results[key] = r
				order = append(order, key)
			}
			for _, p := range []*junitProblem{c.Failure, c.Error, c.Skipped} {
				if p != nil && r.Passed {
					r.Passed = false
					r.Message = trimMessage(p.Message + "\n" + p.Body)
				}
			}
		}
	}
	if runs == 0 {
		return nil, false
	}
	out := make([]TestResult, 0, len(order))
	for _, k := range order {
		out = append(out, *results[k])
	}
	return out, true
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

// ---- helpers ----

func trimMessage(s string) string {
	return text.Truncate(strings.TrimSpace(s), 2000)
}
