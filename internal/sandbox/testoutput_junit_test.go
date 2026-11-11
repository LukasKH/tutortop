package sandbox

import (
	"strings"
	"testing"
)

// Custom toolchains and Python report through JUnit XML, printed between
// markers.
func TestParseJUnit(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		ok     bool
		want   []TestResult // Message is matched as a substring
	}{
		{
			name: "nested suites",
			stdout: report(`<testsuites><testsuite name="outer">
				<testcase classname="a" name="test_one"/>
				<testsuite name="inner"><testcase classname="b" name="test_two"/></testsuite>
			</testsuite></testsuites>`),
			ok:   true,
			want: []TestResult{{Name: "test_one", Passed: true}, {Name: "test_two", Passed: true}},
		},
		{
			name: "failures, errors and skips fail with their message",
			stdout: report(`<testsuite>
				<testcase name="test_fail"><failure message="assert 1 == 2">long trace</failure></testcase>
				<testcase name="test_error"><error message="ZeroDivisionError"/></testcase>
				<testcase name="test_skip"><skipped message="not ready"/></testcase>
				<testcase name="test_edge"/>
			</testsuite>`),
			ok: true,
			want: []TestResult{
				{Name: "test_fail", Message: "assert 1 == 2\nlong trace"},
				{Name: "test_error", Message: "ZeroDivisionError"},
				{Name: "test_skip", Message: "not ready"},
				{Name: "test_edge", Hidden: true, Passed: true},
			},
		},
		{
			// Tests are told apart by class too, so the gate can spot the clash.
			name:   "the same name in two classes is two tests",
			stdout: report(`<testsuite><testcase classname="a" name="test_x"/><testcase classname="b" name="test_x"/></testsuite>`),
			ok:     true,
			want:   []TestResult{{Name: "test_x", Passed: true}, {Name: "test_x", Passed: true}},
		},
		// Each of these means the code didn't build.
		{name: "a collection error", stdout: report(`<testsuite><testcase name="test_one"/><testcase name=""><error message="SyntaxError"/></testcase></testsuite>`)},
		{name: "no report", stdout: "SyntaxError: invalid syntax\n"},
		{name: "an empty report", stdout: report("")},
		{name: "a report without tests", stdout: report(`<testsuites/>`)},
	}
	tc := customToolchain(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tests, ok := tc.parse(c.stdout, map[string]bool{"test_edge": true})
			if ok != c.ok || len(tests) != len(c.want) {
				t.Fatalf("got %+v, %v; want %+v, %v", tests, ok, c.want, c.ok)
			}
			for i, want := range c.want {
				got := tests[i]
				if got.Name != want.Name || got.Passed != want.Passed || got.Hidden != want.Hidden || !strings.Contains(got.Message, want.Message) {
					t.Errorf("test %d is %+v, want %+v", i, got, want)
				}
			}
			if out := stripJUnit(c.stdout); strings.Contains(out, "testsuite") || strings.Contains(out, "TUTORTOP") {
				t.Errorf("the report shows in the output:\n%s", out)
			}
		})
	}
}

// report prints a JUnit report the way junitRun does.
func report(xml string) string {
	return "some output\n" + junitBegin + "\n" + xml + "\n" + junitEnd + "\n"
}
