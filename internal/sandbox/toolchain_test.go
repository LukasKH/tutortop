package sandbox

import (
	"strings"
	"testing"
)

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
		// A volume there would hide the task's files from the run.
		{"cache over the task files", func(s *ToolchainSpec) { s.Cache = "/work/.cache" }, []string{"cache must be an absolute folder outside /work"}},
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
}

func rSpec() ToolchainSpec {
	return ToolchainSpec{
		Language:       "r",
		Name:           "R",
		EditorLanguage: "r",
		Dockerfile:     "FROM docker.io/library/r-base:4\nRUN R -e 'install.packages(\"testthat\")'\n",
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
