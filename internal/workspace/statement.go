package workspace

import (
	"fmt"
	"strings"
)

// Statement is an exercise's problem statement in a guided-assignment shape:
// real-world context and what's already in the starter file first, then a
// step-by-step assignment telling the learner what to build, realistic usage
// examples, and optional notes and background.
type Statement struct {
	Context    string    `json:"context" jsonschema:"2-3 plain sentences shown at the top: the real-world situation (why this code exists) and what's already set up in the starter file (e.g. 'Open job.go: the Job struct and Status constants are already defined for you.'). Name a part of their project only in a project-based track; otherwise describe a realistic situation without inventing an app."`
	Task       string    `json:"task" jsonschema:"1-2 plain imperative sentences written to 'you' introducing the assignment, e.g. 'Complete the functions marked TODO in job.go so the worker can create jobs and check their state.'"`
	Signature  string    `json:"signature,omitempty" jsonschema:"optional: the declaration(s) to implement, without a code fence. Include only for a 1-2 function task when it helps; leave out when the starter file already has multiple stubs named in steps."`
	Steps      []string  `json:"steps" jsonschema:"1-5 actionable instructions written directly to the learner in plain, imperative language ('Complete NewJob to...', 'Implement CanRetry so it returns true when...'). Wrap every file, function, type, field, constant, and literal in backticks. Show concrete sample outputs instead of abstract placeholders like <ID>. Never write dry third-person spec assertions ('X returns Y when Z is equal to W')."`
	Examples   []Example `json:"examples" jsonschema:"2-3 realistic usage examples showing how a caller uses the code and what comes back. Cover the normal case and an edge or error case, with realistic values (not foo or x)."`
	Notes      []string  `json:"notes,omitempty" jsonschema:"optional short bullets in plain words for edge cases, input guarantees, or helpful tips (without giving away the solution), e.g. 'A zero-value Job{} should work safely without panicking.'"`
	Background string    `json:"background,omitempty" jsonschema:"optional markdown: a short refresher on what this task needs that the objective's lesson didn't cover, with a worked example in a code block on a DIFFERENT problem. Leave out when the lesson covers it."`
}

// Markdown renders the statement as the task's README.md.
func (s Statement) Markdown(title, language string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n\n## Assignment\n\n%s\n", title, strings.TrimSpace(s.Context), strings.TrimSpace(s.Task))
	if sig := strings.TrimSpace(s.Signature); sig != "" {
		fmt.Fprintf(&b, "\n```%s\n%s\n```\n", language, sig)
	}
	b.WriteString("\n")
	for i, step := range s.Steps {
		if len(s.Steps) == 1 {
			fmt.Fprintf(&b, "- %s\n", indentStep(step))
		} else {
			fmt.Fprintf(&b, "%d. %s\n", i+1, indentStep(step))
		}
	}
	b.WriteString("\n## Examples\n")
	for _, e := range s.Examples {
		if label := oneLine(e.Title); label != "" {
			fmt.Fprintf(&b, "\n**%s**\n", label)
		}
		fmt.Fprintf(&b, "\n```%s\n%s\n```\n", language, strings.TrimSpace(e.Code))
		if x := strings.TrimSpace(e.Explanation); x != "" {
			fmt.Fprintf(&b, "\n%s\n", x)
		}
	}
	if len(s.Notes) > 0 {
		b.WriteString("\n## Notes\n\n")
		for _, n := range s.Notes {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(n))
		}
	}
	if bg := strings.TrimSpace(s.Background); bg != "" {
		fmt.Fprintf(&b, "\n## Background\n\n%s\n", bg)
	}
	return b.String()
}

func (s Statement) check(p *problems) {
	if strings.TrimSpace(s.Context) == "" {
		p.addf("statement.context is required: explain the scenario and what's already in the starter file")
	}
	if strings.TrimSpace(s.Task) == "" {
		p.addf("statement.task is required: say what the learner will build, e.g. 'Complete NewNote in note.go...'")
	}
	if len(s.Steps) == 0 {
		p.addf("statement.steps needs at least one actionable instruction")
	}
	if len(s.Examples) < 2 {
		p.addf("statement.examples needs at least 2 examples")
	}
	for i, e := range s.Examples {
		if strings.TrimSpace(e.Code) == "" {
			p.addf("statement.examples[%d] needs a code snippet showing how it's used and what it returns", i)
		}
	}
	if b := strings.TrimSpace(s.Background); b != "" && !strings.Contains(b, "```") {
		p.addf("statement.background must show the technique with a worked example in a code block, on a different problem than the task")
	}
}

type Example struct {
	Title       string `json:"title,omitempty" jsonschema:"short plain label for the scenario, e.g. 'Creating a new job' or 'Failed job with retries left'"`
	Code        string `json:"code" jsonschema:"a short realistic snippet in the task's language showing how a caller uses the code and what it returns (as comments), without a code fence"`
	Explanation string `json:"explanation,omitempty" jsonschema:"optional plain sentence explaining why the result is what it is; leave out if the code already makes it obvious, and never just repeat the code"`
}

// indentStep indents continuation lines so multi-line steps (such as sub-bullets)
// stay inside their list item in Markdown.
func indentStep(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = "   " + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// oneLine joins lines with a space so labels stay on a single line.
func oneLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}
