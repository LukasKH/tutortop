package workspace_test

import (
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
		{"reference that isn't a web link", func(c *workspace.Curriculum) { c.Modules[0].References[0].URL = "file:///etc/passwd" }, []string{"needs a full http(s) url"}},
		// Modules and objectives share one id space.
		{"duplicate id", func(c *workspace.Curriculum) { c.Modules[0].Objectives[1].ID = "basics" }, []string{`duplicate id "basics"`}},
		{"every problem at once", func(c *workspace.Curriculum) {
			c.TaskDefaults.Size = "huge"
			c.Modules[0].References = nil
		}, []string{"size must be", "needs references"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cur := plan("a", "b")
			c.change(&cur)
			wantProblems(t, newWorkspace(t).Propose(cur), c.want...)
		})
	}
}

// The app numbers revisions; whatever the tutor sends is ignored.
func TestApproveProposal(t *testing.T) {
	w := newWorkspace(t)
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
}
