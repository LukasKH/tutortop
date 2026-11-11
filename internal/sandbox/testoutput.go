package sandbox

import (
	"strings"

	"github.com/LukasKH/tutortop/internal/text"
)

// reportFormat is how a toolchain's test command reports results. The
// files testoutput_*.go each read one format into the results Run returns.
type reportFormat int

const (
	// The zero value is no format, so a toolchain that forgets one parses
	// nothing instead of guessing.
	goJSON   reportFormat = iota + 1 // `go test -json`
	junitXML                         // a JUnit XML report (junitRun)
)

// reportFormats are the formats by their names in a ToolchainSpec.
var reportFormats = map[string]reportFormat{
	"go-json": goJSON,
	"junit":   junitXML,
}

// script returns the shell command that runs the tests in the task
// directory and prints everything parse needs.
func (tc Toolchain) script() string {
	switch tc.report {
	case goJSON:
		return tc.testCommand
	case junitXML:
		return junitRun(tc.testCommand)
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
		return parseJUnit(stdout, hidden)
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

// trimMessage shortens a failed test's message for the learner.
func trimMessage(s string) string {
	return text.Truncate(strings.TrimSpace(s), 2000)
}
