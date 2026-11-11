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
