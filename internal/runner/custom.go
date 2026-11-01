package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
)

// ToolchainSpec is a language environment the coach sets up for a workspace,
// for anything the app doesn't ship (R, Rust, JavaScript, …). The test command
// must write a JUnit XML report, which nearly every test framework can do, so
// one parser covers them all.
type ToolchainSpec struct {
	Language       string   `yaml:"language" json:"language" jsonschema:"short lowercase id used in the curriculum, e.g. r or rust; not go or python"`
	Name           string   `yaml:"name" json:"name" jsonschema:"shown to the learner, e.g. R"`
	EditorLanguage string   `yaml:"editor_language" json:"editorLanguage" jsonschema:"Monaco editor language id for syntax highlighting, e.g. r, rust, javascript, typescript, java, cpp, csharp, ruby, sql; plaintext if none fits"`
	Dockerfile     string   `yaml:"dockerfile" json:"dockerfile" jsonschema:"Dockerfile for the sandbox image, FROM an official image. Install the language, the test framework, any libraries tasks need and the language server, system-wide. The app appends lines that create /work and switch to user 1000 with HOME=/home/runner. Tests run there with no network."`
	TestCommand    string   `yaml:"test_command" json:"testCommand" jsonschema:"shell command run in /work (the task files) that runs every test file and writes a JUnit XML report to /tmp/junit.xml. It must write the report even when tests fail. Build errors may leave it out. The app stops it after 20 seconds."`
	TestFiles      string   `yaml:"test_files" json:"testFiles" jsonschema:"Go regexp matching the base name of test files, e.g. ^test-.*\\.R$"`
	TestPattern    string   `yaml:"test_pattern" json:"testPattern" jsonschema:"Go regexp matching one test in a test file, with the test's name as group 1 the way it appears in the JUnit report, e.g. test_that\\(\"([^\"]+)\""`
	LanguageServer []string `yaml:"language_server,omitempty" json:"languageServer,omitempty" jsonschema:"optional command (argv) that runs a language server over stdio in /work, installed in the image, for completions and errors in the editor, e.g. [\"R\", \"--slave\", \"-e\", \"languageserver::run()\"]"`
	ExtraFiles     Files    `yaml:"extra_files,omitempty" json:"extraFiles,omitempty" jsonschema:"optional files written next to every task's files, e.g. a package.json or Cargo.toml"`
}

var languageID = regexp.MustCompile(`^[a-z][a-z0-9+#-]{0,19}$`)

// Toolchain validates the spec and turns it into a toolchain. Every problem is
// reported at once so the coach can fix them in one go.
func (s ToolchainSpec) Toolchain() (Toolchain, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if !languageID.MatchString(s.Language) {
		add("language must be a short lowercase id like r or rust")
	}
	if _, ok := Builtin(s.Language); ok {
		add("%s is built in; use it directly", s.Language)
	}
	if strings.TrimSpace(s.Name) == "" {
		add("name is required")
	}
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s.Dockerfile)), "FROM ") {
		add("dockerfile must start with FROM")
	}
	if strings.TrimSpace(s.TestCommand) == "" {
		add("test_command is required")
	}
	testFile, err := regexp.Compile(s.TestFiles)
	if err != nil || s.TestFiles == "" {
		add("test_files must be a valid regexp: %v", err)
	}
	testFunc, err := regexp.Compile("(?m)" + s.TestPattern)
	switch {
	case err != nil || s.TestPattern == "":
		add("test_pattern must be a valid regexp: %v", err)
	case testFunc.NumSubexp() < 1:
		add("test_pattern needs a group that captures the test name")
	}
	for name := range s.ExtraFiles {
		if !isLocalSlash(name) {
			add("extra file %q must be a relative path", name)
		}
	}
	if len(problems) > 0 {
		return Toolchain{}, fmt.Errorf("toolchain problems:\n- %s", strings.Join(problems, "\n- "))
	}

	dockerfile := strings.TrimSpace(s.Dockerfile) + "\n" + customTail
	sum := sha256.Sum256([]byte(dockerfile))
	return Toolchain{
		Language:       s.Language,
		Name:           s.Name,
		EditorLanguage: s.EditorLanguage,
		// Tagged by content, so editing the spec builds a fresh image.
		Image:      "tutortop-" + strings.NewReplacer("+", "p", "#", "s").Replace(s.Language) + ":" + hex.EncodeToString(sum[:6]),
		Dockerfile: dockerfile,
		ExtraFiles: maps.Clone(s.ExtraFiles),
		Script:     func(repeat int) string { return junitScript(s.TestCommand, repeat) },
		Parse:      parseJUnitRuns,
		TestNames: func(filename, src string) []string {
			return append(matchAll(testFunc, src), fileStem(filename))
		},
		LanguageServer: slices.Clone(s.LanguageServer),
		testFile:       testFile,
		testFunc:       testFunc,
	}, nil
}

// customTail is appended to every coach-written Dockerfile, so checks run the
// same way as the built-in images: unprivileged, in /work.
const customTail = `USER root
RUN mkdir -p /work /home/runner && chown 1000:1000 /work /home/runner
ENV HOME=/home/runner
WORKDIR /work
USER 1000:1000
`

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

		var doc junitNode
		if xml.Unmarshal([]byte(strings.TrimSpace(report)), &doc) != nil {
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
				r = &TestResult{Name: c.Name, Hidden: isHidden(c, hidden), Passed: true}
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

// isHidden matches a reported test against the hidden test files: by test
// name, by the file it came from, or by a classname segment naming the file.
func isHidden(c junitCase, hidden map[string]bool) bool {
	if hidden[c.Name] || (c.File != "" && hidden[fileStem(c.File)]) {
		return true
	}
	for seg := range strings.FieldsFuncSeq(c.Classname, func(r rune) bool { return strings.ContainsRune("./:\\", r) }) {
		if hidden[seg] {
			return true
		}
	}
	return false
}

func fileStem(name string) string {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	if i := strings.LastIndex(base, "."); i > 0 {
		return base[:i]
	}
	return base
}

func isLocalSlash(name string) bool {
	return name != "" && !strings.HasPrefix(name, "/") && !slices.Contains(strings.Split(name, "/"), "..")
}
