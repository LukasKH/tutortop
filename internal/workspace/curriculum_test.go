package workspace_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
)

func TestCurriculumValidate(t *testing.T) {
	cases := []struct {
		name   string
		change func(c *workspace.Curriculum)
		want   []string // none for a valid plan
	}{
		{"valid", func(*workspace.Curriculum) {}, nil},
		{"unknown language", func(c *workspace.Curriculum) { c.Language = "cobol" }, []string{"language: cobol is unknown"}},
		{"bad size", func(c *workspace.Curriculum) { c.TaskDefaults.Size = "huge" }, []string{"size must be small, medium or large"}},
		{"bad shape", func(c *workspace.Curriculum) { c.TaskDefaults.Shape = "essay" }, []string{"shape must be kata or project"}},
		{"no modules", func(c *workspace.Curriculum) { c.Modules = nil }, []string{"at least one module"}},
		{"module id", func(c *workspace.Curriculum) { c.Modules[0].ID = "Basics" }, []string{`module id "Basics" must be kebab-case`}},
		{"no objectives", func(c *workspace.Curriculum) { c.Modules[0].Objectives = nil }, []string{"has no objectives"}},
		{"no references", func(c *workspace.Curriculum) { c.Modules[0].References = nil }, []string{"needs references"}},
		{"too many references", func(c *workspace.Curriculum) {
			r := c.Modules[0].References[0]
			c.Modules[0].References = []workspace.Source{r, r, r, r, r}
		}, []string{"4 references at most"}},
		{"reference without a title", func(c *workspace.Curriculum) { c.Modules[0].References[0].Title = " " }, []string{"needs a title"}},
		{"reference that isn't a web link", func(c *workspace.Curriculum) { c.Modules[0].References[0].URL = "file:///etc/passwd" }, []string{"needs a full http(s) url"}},
		{"objective id", func(c *workspace.Curriculum) { c.Modules[0].Objectives[0].ID = "Go Errors" }, []string{"must be lowercase, dotted or dashed"}},
		// Modules and objectives share one id space.
		{"duplicate id", func(c *workspace.Curriculum) { c.Modules[0].Objectives[1].ID = "basics" }, []string{`duplicate id "basics"`}},
		{"objective without a name", func(c *workspace.Curriculum) { c.Modules[0].Objectives[0].Name = "" }, []string{"needs a name"}},
		// The limit counts characters, not bytes.
		{"name at the limit", func(c *workspace.Curriculum) { c.Modules[0].Objectives[0].Name = strings.Repeat("é", 28) }, nil},
		{"name too long", func(c *workspace.Curriculum) { c.Modules[0].Objectives[0].Name = strings.Repeat("e", 29) }, []string{"is too long"}},
		{"unsupported check kind", func(c *workspace.Curriculum) { c.Modules[0].Objectives[0].Verify = "ai-review" }, []string{"verify must be 'tests'"}},
		{"unknown objective language", func(c *workspace.Curriculum) { c.Modules[0].Objectives[0].Language = "cobol" }, []string{`objective "a": cobol is unknown`}},
		{"every problem at once", func(c *workspace.Curriculum) {
			c.TaskDefaults.Size = "huge"
			c.Modules[0].References = nil
		}, []string{"size must be", "needs references"}},
	}
	known := func(lang string) error {
		if lang == "go" || lang == "python" {
			return nil
		}
		return errors.New(lang + " is unknown")
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cur := plan("a", "b")
			c.change(&cur)
			wantProblems(t, cur.Validate(known), c.want...)
		})
	}
}

func TestCurriculumValidateFillsDefaults(t *testing.T) {
	c := plan("a")
	if err := c.Validate(func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if c.TaskDefaults.Size != "medium" || c.TaskDefaults.Shape != "kata" || c.Modules[0].Objectives[0].Verify != "tests" {
		t.Errorf("defaults not filled: %+v, verify %q", c.TaskDefaults, c.Modules[0].Objectives[0].Verify)
	}
}

func TestCurriculumObjectiveLanguage(t *testing.T) {
	c := plan("a", "b")
	c.Modules[0].Objectives[1].Language = "python"
	if _, lang, ok := c.Objective("a"); !ok || lang != "go" {
		t.Errorf("a: %q %v, want the plan's go", lang, ok)
	}
	if _, lang, ok := c.Objective("b"); !ok || lang != "python" {
		t.Errorf("b: %q %v, want its own python", lang, ok)
	}
	if _, _, ok := c.Objective("missing"); ok {
		t.Error("found an objective that isn't in the plan")
	}
}

// The app numbers revisions; whatever the tutor sends is ignored.
func TestApproveProposal(t *testing.T) {
	w := newWorkspace(t)
	if _, err := w.ApproveProposal(); err == nil {
		t.Fatal("approved without a proposal")
	}
	if err := w.Propose(workspace.Curriculum{Language: "go"}); err == nil {
		t.Fatal("an invalid plan was proposed")
	}
	if p, _ := w.Proposal(); p != nil {
		t.Fatal("an invalid plan was stored")
	}

	for want := 1; want <= 2; want++ {
		c := plan("a")
		c.Revision = 7
		if err := w.Propose(c); err != nil {
			t.Fatal(err)
		}
		got, err := w.ApproveProposal()
		if err != nil {
			t.Fatal(err)
		}
		cur, _ := w.Curriculum()
		if got.Revision != want || cur == nil || cur.Revision != want {
			t.Fatalf("approval %d: revision %d, saved %+v", want, got.Revision, cur)
		}
		if p, _ := w.Proposal(); p != nil {
			t.Fatal("the proposal is still pending after approval")
		}
	}

	if err := w.Propose(plan("b")); err != nil {
		t.Fatal(err)
	}
	if err := w.DiscardProposal(); err != nil {
		t.Fatal(err)
	}
	if p, _ := w.Proposal(); p != nil {
		t.Fatal("the proposal is still pending after discarding it")
	}
	if err := w.DiscardProposal(); err != nil {
		t.Errorf("discarding with nothing pending: %v", err)
	}
	if cur, _ := w.Curriculum(); cur.Revision != 2 {
		t.Errorf("discarding changed the plan to revision %d", cur.Revision)
	}
}
