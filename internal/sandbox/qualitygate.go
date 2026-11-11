package sandbox

import (
	"context"
	"fmt"
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
}

type GateStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type GateReport struct {
	Passed bool       `json:"passed"`
	Steps  []GateStep `json:"steps"`
	// Names are the tests the reference run reported, saved with the task.
	Names TestNames `json:"-"`
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

// Gate checks, in order:
//  1. the reference solution passes every test;
//  2. it passes the visible tests alone, which tells visible and hidden
//     tests apart;
//  3. the starter compiles and fails at least one test.
//
// The two reference runs go at once. The starter runs only once they pass,
// as a broken reference makes its result meaningless.
func (r *Runner) Gate(ctx context.Context, in GateInput) (GateReport, error) {
	var rep GateReport
	add := func(name string, ok bool, detail string) { rep.Steps = append(rep.Steps, GateStep{name, ok, detail}) }

	if len(in.Tests) == 0 {
		add("has visible tests", false, "Provide at least one visible test file the learner can read.")
	}
	if len(in.HiddenTests) == 0 {
		add("has hidden tests", false, "Provide at least one hidden test file with edge cases.")
	}
	if len(rep.Steps) > 0 {
		return rep, nil
	}

	var all, visible Result
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		all, err = r.Run(gctx, Job{Toolchain: in.Toolchain, Solution: in.Reference, Tests: in.Tests, HiddenTests: in.HiddenTests})
		return err
	})
	g.Go(func() (err error) {
		visible, err = r.Run(gctx, Job{Toolchain: in.Toolchain, Solution: in.Reference, Tests: in.Tests})
		return err
	})
	if err := g.Wait(); err != nil {
		return rep, err
	}

	if all.Status != StatusPassed {
		add("reference solution passes all tests", false, describe(all)+"The reference must pass every visible and hidden test.")
		return rep, nil
	}
	add("reference solution passes all tests", true, "")
	if visible.Status != StatusPassed {
		add("reference solution passes the visible tests on their own", false,
			describe(visible)+"The learner's Run uses only the visible tests, so they must work without the hidden files.")
		return rep, nil
	}
	names, problem := splitNames(all.Tests, visible.Tests)
	if problem != "" {
		add("visible and hidden tests have their own names", false, problem)
		return rep, nil
	}
	rep.Names = names
	add(fmt.Sprintf("visible tests pass on their own (%d visible, %d hidden)", len(names.Visible), len(names.Hidden)), true, "")

	starter, err := r.Run(ctx, Job{Toolchain: in.Toolchain, Solution: in.Starter, Tests: in.Tests, HiddenTests: in.HiddenTests})
	if err != nil {
		return rep, err
	}
	switch starter.Status {
	case StatusPassed:
		add("starter code fails at least one test", false, "The starter already passes every test, so there's nothing to solve.")
	case StatusError:
		add("starter code compiles", false, "The starter must compile so the learner sees test failures, not build errors.\n"+text.Tail(starter.Output, 1500))
	case StatusFailed:
		p, t := starter.Counts()
		add("starter code fails at least one test", true, fmt.Sprintf("%d/%d passing", p, t))
		rep.Passed = true
	}
	return rep, nil
}

// splitNames names the tests of a full run, hidden being those the
// visible-only run didn't report.
func splitNames(all, visible []TestResult) (TestNames, string) {
	isVisible := map[string]bool{}
	for _, t := range visible {
		isVisible[t.Name] = true
	}
	var names TestNames
	seen := map[string]bool{}
	for _, t := range all {
		if seen[t.Name] {
			return names, fmt.Sprintf("Two tests are called %q. Give every test, visible or hidden, its own name.", t.Name)
		}
		seen[t.Name] = true
		if isVisible[t.Name] {
			names.Visible = append(names.Visible, t.Name)
		} else {
			names.Hidden = append(names.Hidden, t.Name)
		}
	}
	if len(names.Hidden) == 0 {
		return names, "None of the hidden tests ran. Name the hidden test files so the test framework finds them."
	}
	return names, ""
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
