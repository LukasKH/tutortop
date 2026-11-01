package workspace

import (
	"strings"
	"testing"
)

func TestObjectiveNames(t *testing.T) {
	plan := func(name string) Curriculum {
		return Curriculum{Language: "go", Modules: []Module{{ID: "m", Title: "M", References: []Source{{Title: "Go docs", URL: "https://go.dev/doc/"}}, Objectives: []Objective{
			{ID: "go.errors.wrap", Name: name, Title: "Return wrapped errors that callers can inspect with errors.Is"},
		}}}}
	}
	ok := func(string) error { return nil }

	c := plan("Wrapping errors")
	if err := c.Validate(ok); err != nil {
		t.Errorf("a short name was rejected: %v", err)
	}
	for _, name := range []string{"", "Return wrapped errors that callers can inspect"} {
		c := plan(name)
		if err := c.Validate(ok); err == nil || !strings.Contains(err.Error(), "name") {
			t.Errorf("name %q: got %v, want a problem about the name", name, err)
		}
	}
}

// A proposed plan says where each module comes from, with real links.
func TestReferences(t *testing.T) {
	ok := func(string) error { return nil }
	plan := func(refs ...Source) Curriculum {
		return Curriculum{Language: "go", Modules: []Module{{ID: "m", Title: "M", References: refs, Objectives: []Objective{{ID: "go.a", Name: "A", Title: "Do A"}}}}}
	}

	c := plan()
	if err := c.Validate(ok); err == nil || !strings.Contains(err.Error(), "needs references") {
		t.Errorf("a plan without references: %v", err)
	}
	for _, bad := range []Source{{Title: "", URL: "https://go.dev/"}, {Title: "Spec", URL: "go.dev/ref/spec"}, {Title: "Spec", URL: "ftp://go.dev/"}} {
		c := plan(bad)
		if err := c.Validate(ok); err == nil || !strings.Contains(err.Error(), "reference") {
			t.Errorf("reference %+v was accepted: %v", bad, err)
		}
	}
	c = plan(Source{Title: "The Go spec: Errors", URL: "https://go.dev/ref/spec#Errors"})
	if err := c.Validate(ok); err != nil {
		t.Errorf("a good reference was rejected: %v", err)
	}
	if got := c.URLs(); len(got) != 1 || got[0] != "https://go.dev/ref/spec#Errors" {
		t.Errorf("URLs = %q", got)
	}
}
