package runner

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/LukasKH/tutortop/internal/text"
	"golang.org/x/sync/errgroup"
)

// GateInput is everything an AI-authored task must provide so the app can
// prove its checks are trustworthy before a learner sees it.
type GateInput struct {
	Toolchain   Toolchain
	Starter     Files
	Reference   Files
	Tests       Files
	HiddenTests Files
	Mutants     []Mutant
}

// Mutant is a plausible wrong solution the tests must reject.
type Mutant struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Files       Files  `json:"files"`
}

type GateStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type GateReport struct {
	Passed bool       `json:"passed"`
	Steps  []GateStep `json:"steps"`
}

// String is the report the authoring agent reads when fixing a task.
func (g GateReport) String() string {
	var b strings.Builder
	if g.Passed {
		b.WriteString("Quality gate PASSED.\n")
	} else {
		b.WriteString("Quality gate FAILED. Fix the problems below and call create_task again.\n")
	}
	for _, s := range g.Steps {
		mark := "✓"
		if !s.OK {
			mark = "✗"
		}
		fmt.Fprintf(&b, "%s %s", mark, s.Name)
		if s.Detail != "" {
			fmt.Fprintf(&b, "\n    %s", strings.ReplaceAll(s.Detail, "\n", "\n    "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

const minMutants = 2

// Gate checks, in order:
//  1. the reference solution passes every check three times, shuffled;
//  2. the starter compiles and fails at least one check;
//  3. every mutant compiles and fails at least one check.
func (r *Runner) Gate(ctx context.Context, in GateInput) (GateReport, error) {
	var rep GateReport
	add := func(name string, ok bool, detail string) { rep.Steps = append(rep.Steps, GateStep{name, ok, detail}) }

	if len(in.Tests) == 0 {
		add("has visible tests", false, "Provide at least one visible test file the learner can read.")
	}
	if len(in.HiddenTests) == 0 {
		add("has hidden tests", false, "Provide at least one hidden test file with edge cases.")
	}
	if len(in.Mutants) < minMutants {
		add("has mutants", false, fmt.Sprintf("Provide at least %d mutants: plausible wrong solutions (off-by-one, missed edge case, wrong type of error, …).", minMutants))
	}
	if len(rep.Steps) > 0 {
		return rep, nil
	}

	run := func(ctx context.Context, sol Files, repeat int) (Result, error) {
		return r.Run(ctx, Job{Toolchain: in.Toolchain, Solution: sol, Tests: in.Tests, HiddenTests: in.HiddenTests, Repeat: repeat})
	}

	// 1. The reference passes every test three times, in shuffled order where
	// the language allows, so flaky or order-dependent tests are caught.
	res, err := run(ctx, in.Reference, 3)
	if err != nil {
		return rep, err
	}
	if res.Status != StatusPassed {
		add("reference solution passes all tests (3 shuffled repetitions)", false,
			describe(res)+"If the reference passes on its own, the tests are flaky or depend on order: remove randomness, wall-clock time, map ordering and shared state between tests.")
		return rep, nil
	}
	add("reference solution passes all tests (3 shuffled repetitions)", true, "")

	// 2. The starter compiles and fails.
	res, err = run(ctx, in.Starter, 1)
	if err != nil {
		return rep, err
	}
	switch res.Status {
	case StatusPassed:
		add("starter code fails at least one test", false, "The starter already passes every test, so there's nothing to solve.")
	case StatusError:
		add("starter code compiles", false, "The starter must compile so the learner sees test failures, not build errors.\n"+text.Tail(res.Output, 1500))
	default:
		p, t := res.Counts()
		add("starter code fails at least one test", true, fmt.Sprintf("%d/%d passing", p, t))
	}

	// 3. Every mutant compiles and is caught.
	steps := make([]GateStep, len(in.Mutants))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for i, m := range in.Mutants {
		g.Go(func() error {
			sol := maps.Clone(in.Reference)
			maps.Copy(sol, m.Files)
			res, err := run(gctx, sol, 1)
			if err != nil {
				return err
			}
			name := fmt.Sprintf("mutant %q is caught", m.Name)
			switch res.Status {
			case StatusPassed:
				steps[i] = GateStep{name, false, "Tests pass for this wrong solution (" + m.Description + "). Add a test that catches it."}
			case StatusError:
				steps[i] = GateStep{name, false, "Mutant doesn't compile, so it proves nothing. Make it a compiling, plausible mistake.\n" + text.Tail(res.Output, 800)}
			default:
				steps[i] = GateStep{name, true, ""}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return rep, err
	}
	rep.Steps = append(rep.Steps, steps...)

	rep.Passed = true
	for _, s := range rep.Steps {
		rep.Passed = rep.Passed && s.OK
	}
	return rep, nil
}

func describe(res Result) string {
	if res.Status == StatusError {
		return "Build or run error:\n" + text.Tail(res.Output, 1500)
	}
	var b strings.Builder
	for _, t := range res.Tests {
		if !t.Passed {
			fmt.Fprintf(&b, "%s failed: %s\n", t.Name, text.Tail(t.Message, 400))
		}
	}
	return b.String()
}
