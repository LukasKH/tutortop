package sandbox

import (
	"strings"
	"testing"
)

// Custom toolchains and Python report through JUnit XML, printed between
// markers once per repetition.
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
		{
			name: "one failed repetition fails the test",
			stdout: report(`<testsuite><testcase name="test_one"/><testcase name="test_two"/></testsuite>`) +
				report(`<testsuite><testcase name="test_one"><failure message="second run"/></testcase><testcase name="test_two"/></testsuite>`),
			ok:   true,
			want: []TestResult{{Name: "test_one", Message: "second run"}, {Name: "test_two", Passed: true}},
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

func TestToolchainSpec(t *testing.T) {
	cases := []struct {
		name   string
		change func(s *ToolchainSpec)
		want   []string // none for a valid spec
	}{
		{"valid", func(*ToolchainSpec) {}, nil},
		{"language escaping its folder", func(s *ToolchainSpec) { s.Language = "../r" }, []string{"language must be a short lowercase id"}},
		{"test files as a path", func(s *ToolchainSpec) { s.TestFiles = "tests/test-*.R" }, []string{"test_files must be a glob"}},
		{"extra file outside the task", func(s *ToolchainSpec) { s.ExtraFiles = Files{"../DESCRIPTION": ""} }, []string{`extra file "../DESCRIPTION" must be a relative path`}},
		{"every problem at once", func(s *ToolchainSpec) { s.Name, s.TestCommand = "", "" }, []string{"name is required", "test_command is required"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := rSpec()
			c.change(&spec)
			_, err := spec.Toolchain()
			if len(c.want) == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatalf("no error, want %q", c.want)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error doesn't mention %q:\n%v", w, err)
				}
			}
		})
	}
}

func TestToolchainFromSpec(t *testing.T) {
	tc := customToolchain(t)
	// Checks run unprivileged in /work, whatever the tutor's Dockerfile does.
	if !strings.HasSuffix(tc.Dockerfile, "USER 1000:1000\n") || !strings.Contains(tc.Dockerfile, "WORKDIR /work") {
		t.Errorf("the Dockerfile doesn't end as the sandbox user in /work:\n%s", tc.Dockerfile)
	}
	for name, want := range map[string]bool{"test-sum.R": true, "tests/test-sum.R": true, "sum.R": false, "test-sum.py": false} {
		if tc.IsTestFile(name) != want {
			t.Errorf("IsTestFile(%q) = %v, want %v", name, !want, want)
		}
	}

	// Images are tagged by content, so an edited spec builds a fresh one.
	same := customToolchain(t)
	edited := rSpec()
	edited.Dockerfile += "RUN echo more\n"
	other, err := edited.Toolchain()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tc.Image, "tutortop-r:") || same.Image != tc.Image || other.Image == tc.Image {
		t.Errorf("images %q, %q and edited %q", tc.Image, same.Image, other.Image)
	}
	// Image names can't hold + or #.
	for lang, prefix := range map[string]string{"c++": "tutortop-cpp:", "c#": "tutortop-cs:"} {
		spec := rSpec()
		spec.Language = lang
		tc, err := spec.Toolchain()
		if err != nil || !strings.HasPrefix(tc.Image, prefix) {
			t.Errorf("%s image %q (%v), want prefix %s", lang, tc.Image, err, prefix)
		}
	}
}

func rSpec() ToolchainSpec {
	return ToolchainSpec{
		Language:       "r",
		Name:           "R",
		EditorLanguage: "r",
		Dockerfile:     "FROM r-base:4\nRUN R -e 'install.packages(\"testthat\")'\n",
		TestCommand:    "Rscript -e 'testthat::test_dir(\".\", reporter = testthat::JunitReporter$new(file = \"/tmp/junit.xml\"))'",
		TestFiles:      "test-*.R",
	}
}

func customToolchain(t *testing.T) Toolchain {
	t.Helper()
	tc, err := rSpec().Toolchain()
	if err != nil {
		t.Fatal(err)
	}
	return tc
}

// report prints a JUnit report the way junitScript does.
func report(xml string) string {
	return "some output\n" + junitBegin + "\n" + xml + "\n" + junitEnd + "\n"
}
