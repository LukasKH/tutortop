package workspace

import (
	"fmt"
	"strings"
)

// Statement is a task's problem statement in a fixed shape, laid out like a
// coding-practice problem: what to write first, then the exact rules, worked
// examples with input and output, and the constraints the hidden tests check.
// The why and any background come last; the objective's lesson teaches the
// concept.
type Statement struct {
	Task         string    `json:"task" jsonschema:"what to write, in one or two imperative sentences that name the function and the result, e.g. 'Change NewNote so that an empty or whitespace-only title returns an error that wraps ErrEmptyTitle.' Not backstory: say what to do"`
	Signature    string    `json:"signature" jsonschema:"the exact declaration(s) the learner implements, as code without a code fence"`
	Requirements []string  `json:"requirements" jsonschema:"2-6 exact rules the code must follow, each one checkable, e.g. 'The error message is exactly new note: empty title.' Specific enough to know what to write, without giving the code"`
	Examples     []Example `json:"examples" jsonschema:"2-4 worked examples: an input, the exact output, and an explanation when it isn't obvious. Cover the normal case and at least one error or edge case"`
	Constraints  []string  `json:"constraints,omitempty" jsonschema:"limits on the input and the edge cases the hidden tests check, described without giving away the solution, e.g. 'title may contain any Unicode whitespace'"`
	Context      string    `json:"context" jsonschema:"one or two sentences: where this code is used in the learner's mission"`
	Background   string    `json:"background,omitempty" jsonschema:"optional markdown: a short refresher on what this task needs that the objective's lesson didn't cover, with a worked example in a code block on a DIFFERENT problem. Leave out when the lesson covers it"`
}

// Markdown renders the statement as the task's README.md.
func (s Statement) Markdown(title, language string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n\n", title, strings.TrimSpace(s.Task))
	fmt.Fprintf(&b, "```%s\n%s\n```\n\n", language, strings.TrimSpace(s.Signature))
	b.WriteString("## Requirements\n\n")
	for _, r := range s.Requirements {
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(r))
	}
	for i, e := range s.Examples {
		// One paragraph, a line each: Input, Output, Explanation.
		fmt.Fprintf(&b, "\n## Example %d\n\n**Input:** %s  \n**Output:** %s", i+1, inlineCode(e.Input), inlineCode(e.Output))
		if x := oneLine(e.Explanation); x != "" {
			fmt.Fprintf(&b, "  \n**Explanation:** %s", x)
		}
		b.WriteString("\n")
	}
	if len(s.Constraints) > 0 {
		b.WriteString("\n## Constraints\n\n")
		for _, c := range s.Constraints {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(c))
		}
	}
	fmt.Fprintf(&b, "\n## Why\n\n%s\n", strings.TrimSpace(s.Context))
	if bg := strings.TrimSpace(s.Background); bg != "" {
		fmt.Fprintf(&b, "\n## Background\n\n%s\n", bg)
	}
	return b.String()
}

func (s Statement) check(p *problems) {
	if strings.TrimSpace(s.Task) == "" {
		p.addf("statement.task is required: say what to write, e.g. 'Change NewNote so that …'")
	}
	if strings.TrimSpace(s.Context) == "" {
		p.addf("statement.context is required")
	}
	if strings.TrimSpace(s.Signature) == "" {
		p.addf("statement.signature is required")
	}
	if len(s.Requirements) == 0 {
		p.addf("statement.requirements needs at least one exact rule")
	}
	if len(s.Examples) < 2 {
		p.addf("statement.examples needs at least 2 examples")
	}
	for i, e := range s.Examples {
		if strings.TrimSpace(e.Input) == "" || strings.TrimSpace(e.Output) == "" {
			p.addf("statement.examples[%d] needs an input and an output", i)
		}
	}
	if b := strings.TrimSpace(s.Background); b != "" && !strings.Contains(b, "```") {
		p.addf("statement.background must show the technique with a worked example in a code block, on a different problem than the task")
	}
}

type Example struct {
	Input       string `json:"input" jsonschema:"the call, as code, e.g. NewNote(2, \"\", \"x\")"`
	Output      string `json:"output" jsonschema:"exactly what it returns, as code, e.g. Note{}, error \"new note: empty title\""`
	Explanation string `json:"explanation,omitempty" jsonschema:"why, in a sentence, when it isn't obvious"`
}

// inlineCode puts a value on one line in a code span, with a longer fence
// when the value itself contains backticks.
func inlineCode(s string) string {
	s = oneLine(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}

// oneLine joins lines with a space. Spaces inside stay as written: in an
// example like NewNote(2, "   ", "") they're the point.
func oneLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}
