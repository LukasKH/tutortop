package workspace

import (
	"strings"
	"testing"
)

func TestStatementMarkdown(t *testing.T) {
	s := Statement{
		Task:         "Change `NewNote` so that an empty title returns an error that wraps `ErrEmptyTitle`.",
		Signature:    "func NewNote(id int, title, body string) (Note, error)",
		Requirements: []string{"The message is exactly `new note: empty title`."},
		Examples: []Example{
			{Input: `NewNote(1, "Read", "")`, Output: `Note{ID: 1, Title: "Read"}, nil`},
			{Input: `NewNote(2, "   ", "")`, Output: "Note{}, `new note: empty title`", Explanation: "Only whitespace counts as empty."},
		},
		Constraints: []string{"title may contain any Unicode whitespace"},
		Context:     "Your notes app rejects empty titles before saving.",
	}
	var p problems
	s.check(&p)
	if err := p.err("statement"); err != nil {
		t.Fatal(err)
	}
	md := s.Markdown("Wrap the empty-title error", "go")
	for _, want := range []string{
		"# Wrap the empty-title error\n\nChange `NewNote`",
		"```go\nfunc NewNote",
		"## Example 2\n\n**Input:** `NewNote(2, \"   \", \"\")`  \n**Output:** `` Note{}, `new note: empty title` ``  \n**Explanation:** Only whitespace counts as empty.",
		"## Constraints\n\n- title may contain",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	// The task comes before the why.
	if strings.Index(md, "## Why") < strings.Index(md, "## Example 1") {
		t.Errorf("Why should come after the examples:\n%s", md)
	}
}

func TestStatementNeedsTaskAndExamples(t *testing.T) {
	var p problems
	Statement{Signature: "func F()", Requirements: []string{"x"}, Examples: []Example{{Input: "F()"}}, Context: "c"}.check(&p)
	msg := p.err("statement").Error()
	for _, want := range []string{"statement.task", "at least 2 examples", "needs an input and an output"} {
		if !strings.Contains(msg, want) {
			t.Errorf("problems %q lack %q", msg, want)
		}
	}
}
