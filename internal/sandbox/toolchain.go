package sandbox

import (
	"cmp"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/LukasKH/tutortop/internal/decode"
)

// Toolchain knows how to run one language's tests and read the results. Only
// ToolchainSpec.Toolchain makes one, so every toolchain has been validated:
// the built-in ones from builtins/*.yaml, the others from the tutor's specs.
type Toolchain struct {
	Language string // id used in the curriculum, e.g. "go"
	Name     string // shown to people, e.g. "Go"
	// EditorLanguage is the Monaco language id for syntax highlighting.
	EditorLanguage string
	// Image is the container image checks run in, built locally from
	// Dockerfile the first time it is needed. Its tag is a hash of the
	// Dockerfile, so a changed Dockerfile builds a fresh image.
	Image      string
	Dockerfile string
	// ExtraFiles are written next to the task files before running.
	ExtraFiles map[string]string
	// LanguageServer is the command that runs a language server over stdio
	// inside the image, for completions and diagnostics in the editor.
	// Empty means the editor gets syntax highlighting only.
	LanguageServer []string

	report reportFormat
	// testCommand runs the tests in /work. A junitXML one writes its report
	// to /tmp/junit.xml.
	testCommand string
	testFiles   string // glob matching a test file's base name
	// cache is a folder in the image that runs sharing a Job.Cache keep in a
	// volume, such as Go's build cache. Empty for none.
	cache string
}

// Builtin returns a toolchain the app ships with. Other languages are set up
// per workspace with a ToolchainSpec.
func Builtin(language string) (Toolchain, bool) {
	tc, ok := builtins[language]
	return tc, ok
}

// BuiltinLanguages lists the ids of the languages the app ships with, sorted.
func BuiltinLanguages() []string { return slices.Sorted(maps.Keys(builtins)) }

// IsTestFile reports whether a file name is one of this language's test files.
func (tc Toolchain) IsTestFile(name string) bool {
	ok, _ := path.Match(tc.testFiles, path.Base(name))
	return ok
}

// ToolchainSpec describes a language environment as data: the built-in ones
// in builtins/*.yaml, and the ones the tutor sets up for a workspace with
// set_toolchain. The tutor's test command must write a JUnit XML report,
// which nearly every test framework can do, so one parser covers them all.
type ToolchainSpec struct {
	Language       string   `yaml:"language" json:"language" jsonschema:"short lowercase id used in the curriculum, e.g. r; not a language the app ships"`
	Name           string   `yaml:"name" json:"name" jsonschema:"shown to the learner, e.g. R"`
	EditorLanguage string   `yaml:"editor_language" json:"editorLanguage" jsonschema:"Monaco editor language id for syntax highlighting, e.g. r, rust, javascript, typescript, java, cpp, csharp, ruby, sql; plaintext if none fits"`
	Dockerfile     string   `yaml:"dockerfile" json:"dockerfile" jsonschema:"Dockerfile for the sandbox image, FROM an official image by its full name, e.g. docker.io/library/node:24-alpine (Podman refuses short names). Install the language, the test framework, any libraries tasks need and the language server, system-wide. The app appends lines that create /work and switch to user 1000 with HOME=/home/runner. Tests run there with no network."`
	TestCommand    string   `yaml:"test_command" json:"testCommand" jsonschema:"shell command run in /work (the task files) that runs every test file and writes a JUnit XML report to /tmp/junit.xml. It must write the report even when tests fail. Build errors may leave it out. The app stops it after 20 seconds."`
	TestFiles      string   `yaml:"test_files" json:"testFiles" jsonschema:"glob matching the base name of test files, e.g. test-*.R or *.test.js; the test framework must find files named this way"`
	LanguageServer []string `yaml:"language_server,omitempty" json:"languageServer,omitempty" jsonschema:"optional command (argv) that runs a language server over stdio in /work, installed in the image, for completions and errors in the editor, e.g. [\"R\", \"--slave\", \"-e\", \"languageserver::run()\"]. If it doesn't answer, the language is saved without it."`
	ExtraFiles     Files    `yaml:"extra_files,omitempty" json:"extraFiles,omitempty" jsonschema:"optional files written next to every task's files, e.g. a package.json or Cargo.toml"`

	// The fields below tune the built-in languages. set_toolchain's schema
	// leaves them out, so the tutor's languages report JUnit and keep no
	// cache.

	// Report is how the test command reports results: junit (the default)
	// or go-json (`go test -json`).
	Report string `yaml:"report,omitempty" json:"-"`
	// Cache is a folder in the image that a workspace's runs keep between
	// them, such as a compiler's cache (Job.Cache).
	Cache string `yaml:"cache,omitempty" json:"-"`
}

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
	report, ok := reportFormats[cmp.Or(s.Report, "junit")]
	if !ok {
		add("report must be junit or go-json")
	}
	// The volume would hide the task files, or the whole image, from the run.
	if s.Cache != "" && (!path.IsAbs(s.Cache) || path.Clean(s.Cache) == "/" || isUnder(path.Clean(s.Cache), "/work")) {
		add("cache must be an absolute folder outside /work")
	}
	if len(problems) > 0 {
		return Toolchain{}, fmt.Errorf("toolchain problems:\n- %s", strings.Join(problems, "\n- "))
	}

	dockerfile := strings.TrimSpace(s.Dockerfile) + "\n" + dockerfileTail
	sum := sha256.Sum256([]byte(dockerfile))
	return Toolchain{
		Language:       s.Language,
		Name:           s.Name,
		EditorLanguage: s.EditorLanguage,
		// Tagged by content, so editing the spec builds a fresh image.
		Image:          "tutortop-" + nameSafe(s.Language) + ":" + hex.EncodeToString(sum[:6]),
		Dockerfile:     dockerfile,
		ExtraFiles:     maps.Clone(s.ExtraFiles),
		LanguageServer: slices.Clone(s.LanguageServer),
		report:         report,
		testCommand:    s.TestCommand,
		testFiles:      s.TestFiles,
		cache:          s.Cache,
	}, nil
}

// ---- built-in languages ----

//go:embed builtins/*.yaml
var builtinSpecs embed.FS

// builtins are the languages the app ships with, by id. The app's
// descriptions and prompts for the agent name them through BuiltinLanguages.
// Struct tags name them by hand, and the shared prompt has each one's
// conventions, so a new one is added to both by hand. docs/architecture.md
// has the steps for adding one.
var builtins = loadBuiltins()

// loadBuiltins turns builtins/<language>.yaml into toolchains. They ship
// with the app, so a broken one is a bug: it panics as the app starts, and
// fails every test in this package.
func loadBuiltins() map[string]Toolchain {
	paths, err := fs.Glob(builtinSpecs, "builtins/*.yaml")
	if err != nil {
		panic(err)
	}
	out := map[string]Toolchain{}
	for _, p := range paths {
		tc, err := loadBuiltin(p)
		if err != nil {
			panic(fmt.Sprintf("%s: %v", p, err))
		}
		out[tc.Language] = tc
	}
	return out
}

func loadBuiltin(p string) (Toolchain, error) {
	b, err := builtinSpecs.ReadFile(p)
	if err != nil {
		return Toolchain{}, err
	}
	spec, err := decode.YAML[ToolchainSpec](b)
	if err != nil {
		return Toolchain{}, err
	}
	if want := strings.TrimSuffix(path.Base(p), ".yaml"); spec.Language != want {
		return Toolchain{}, fmt.Errorf("language %q, want %q as in the file name", spec.Language, want)
	}
	return spec.Toolchain()
}

var languageID = regexp.MustCompile(`^[a-z][a-z0-9+#-]{0,19}$`)

// dockerfileTail is appended to every toolchain's Dockerfile, so checks run
// the same way in every image: unprivileged, in /work.
const dockerfileTail = `USER root
RUN mkdir -p /work /home/runner && chown 1000:1000 /work /home/runner
ENV HOME=/home/runner
WORKDIR /work
USER 1000:1000
`

// nameSafe turns a language id into a part of an image or volume name, which
// can't hold + or #.
func nameSafe(language string) string {
	return strings.NewReplacer("+", "p", "#", "s").Replace(language)
}

// isUnder reports whether clean path p is dir or inside it.
func isUnder(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+"/")
}
