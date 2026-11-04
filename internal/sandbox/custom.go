package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
)

// ToolchainSpec is a language environment the tutor sets up for a workspace,
// for any language the app doesn't ship. The test command must write a JUnit
// XML report, which nearly every test framework can do, so one parser covers
// them all.
type ToolchainSpec struct {
	Language       string   `yaml:"language" json:"language" jsonschema:"short lowercase id used in the curriculum, e.g. r; not a language the app ships"`
	Name           string   `yaml:"name" json:"name" jsonschema:"shown to the learner, e.g. R"`
	EditorLanguage string   `yaml:"editor_language" json:"editorLanguage" jsonschema:"Monaco editor language id for syntax highlighting, e.g. r, rust, javascript, typescript, java, cpp, csharp, ruby, sql; plaintext if none fits"`
	Dockerfile     string   `yaml:"dockerfile" json:"dockerfile" jsonschema:"Dockerfile for the sandbox image, FROM an official image. Install the language, the test framework, any libraries tasks need and the language server, system-wide. The app appends lines that create /work and switch to user 1000 with HOME=/home/sandbox. Tests run there with no network."`
	TestCommand    string   `yaml:"test_command" json:"testCommand" jsonschema:"shell command run in /work (the task files) that runs every test file and writes a JUnit XML report to /tmp/junit.xml. It must write the report even when tests fail. Build errors may leave it out. The app stops it after 20 seconds."`
	TestFiles      string   `yaml:"test_files" json:"testFiles" jsonschema:"glob matching the base name of test files, e.g. test-*.R or *.test.js; the test framework must find files named this way"`
	LanguageServer []string `yaml:"language_server,omitempty" json:"languageServer,omitempty" jsonschema:"optional command (argv) that runs a language server over stdio in /work, installed in the image, for completions and errors in the editor, e.g. [\"R\", \"--slave\", \"-e\", \"languageserver::run()\"]. If it doesn't answer, the language is saved without it."`
	ExtraFiles     Files    `yaml:"extra_files,omitempty" json:"extraFiles,omitempty" jsonschema:"optional files written next to every task's files, e.g. a package.json or Cargo.toml"`
}

var languageID = regexp.MustCompile(`^[a-z][a-z0-9+#-]{0,19}$`)

// Toolchain validates the spec and turns it into a toolchain. Every problem is
// reported at once so the tutor can fix them in one go.
func (s ToolchainSpec) Toolchain() (Toolchain, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if !languageID.MatchString(s.Language) {
		add("language must be a short lowercase id like r or rust")
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
	if _, err := path.Match(s.TestFiles, ""); err != nil || s.TestFiles == "" || strings.ContainsAny(s.TestFiles, "^$/") {
		add("test_files must be a glob matching a file's base name, like test_*.py, not a regexp or a path")
	}
	for name := range s.ExtraFiles {
		if !fs.ValidPath(path.Clean(name)) {
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
		Image:          "tutortop-" + strings.NewReplacer("+", "p", "#", "s").Replace(s.Language) + ":" + hex.EncodeToString(sum[:6]),
		Dockerfile:     dockerfile,
		ExtraFiles:     maps.Clone(s.ExtraFiles),
		script:         func(repeat int) string { return junitScript(s.TestCommand, repeat) },
		parse:          parseJUnitRuns,
		readable:       stripJUnit,
		LanguageServer: slices.Clone(s.LanguageServer),
		testFiles:      s.TestFiles,
	}, nil
}

// customTail is appended to every tutor-written Dockerfile, so checks run the
// same way as the built-in images: unprivileged, in /work.
const customTail = `USER root
RUN mkdir -p /work /home/runner && chown 1000:1000 /work /home/runner
ENV HOME=/home/runner
WORKDIR /work
USER 1000:1000
`
