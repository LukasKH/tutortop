package sandbox

import (
	"cmp"
	"encoding/xml"
	"slices"
	"strings"

	"github.com/LukasKH/tutortop/internal/decode"
)

// junitRun prints the report between these markers, so it can be told
// apart from the test command's own output.
const (
	junitBegin = "===TUTORTOP-JUNIT-BEGIN==="
	junitEnd   = "===TUTORTOP-JUNIT-END==="
)

// junitRun runs the test command and prints its report between markers.
func junitRun(command string) string {
	return "rm -f /tmp/junit.xml; ( " + command + " ) 2>&1; " +
		"echo; echo '" + junitBegin + "'; cat /tmp/junit.xml 2>/dev/null; echo; echo '" + junitEnd + "'"
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

// parseJUnit reads the report junitRun prints. A run without a readable
// report, or with no tests, means the code didn't build.
func parseJUnit(stdout string, hidden map[string]bool) ([]TestResult, bool) {
	_, rest, found := strings.Cut(stdout, junitBegin)
	if !found {
		return nil, false
	}
	report, _, _ := strings.Cut(rest, junitEnd)
	doc, err := decode.XML[junitNode]([]byte(strings.TrimSpace(report)))
	if err != nil {
		return nil, false
	}
	cases := doc.cases()
	if len(cases) == 0 {
		return nil, false
	}
	out := make([]TestResult, 0, len(cases))
	for _, c := range cases {
		// A collection error (e.g. a syntax error) shows up as an errored
		// case with no name: the code didn't build.
		if c.Name == "" && c.Error != nil {
			return nil, false
		}
		r := TestResult{Name: c.Name, Hidden: hidden[c.Name], Passed: true}
		if p := cmp.Or(c.Failure, c.Error, c.Skipped); p != nil {
			r.Passed = false
			r.Message = trimMessage(p.Message + "\n" + p.Body)
		}
		out = append(out, r)
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
