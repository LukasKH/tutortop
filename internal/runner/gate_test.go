package runner

import (
	"strings"
	"testing"
)

func TestGate(t *testing.T) {
	const (
		reference = "package task\n\nfunc Sum(a, b int) int { return a + b }\n"
		starter   = "package task\n\nfunc Sum(a, b int) int { return 0 }\n"
	)
	good := func() GateInput {
		return GateInput{
			Toolchain:   goToolchain,
			Starter:     Files{"sum.go": starter},
			Reference:   Files{"sum.go": reference},
			Tests:       Files{"sum_test.go": test(`func TestSum(t *testing.T) { if Sum(1, 2) != 3 { t.Fatal("Sum(1, 2) != 3") } }`)},
			HiddenTests: Files{"sum_hidden_test.go": test(`func TestNegative(t *testing.T) { if Sum(-1, -2) != -3 { t.Fatal("Sum(-1, -2) != -3") } }`)},
			Mutants: []Mutant{
				{Name: "ignores b", Files: Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a }\n"}},
				{Name: "off by one", Files: Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a + b + 1 }\n"}},
			},
		}
	}
	cases := []struct {
		name   string
		change func(in *GateInput)
		passed bool
		steps  []string // every step, failed ones marked with a leading ✗
	}{
		{
			name:   "a sound task",
			change: func(*GateInput) {},
			passed: true,
			steps: []string{
				"reference solution passes all tests",
				"visible tests pass on their own (1 visible, 1 hidden)",
				"starter code fails at least one test",
				`mutant "ignores b" is caught`,
				`mutant "off by one" is caught`,
			},
		},
		{
			name: "missing parts are reported before anything runs",
			change: func(in *GateInput) {
				in.Tests, in.HiddenTests, in.Mutants = nil, nil, in.Mutants[:1]
			},
			steps: []string{"✗has visible tests", "✗has hidden tests", "✗has mutants"},
		},
		{
			name: "a flaky reference",
			change: func(in *GateInput) {
				in.Reference = Files{"sum.go": "package task\n\nvar calls int\n\nfunc Sum(a, b int) int { calls++; if calls == 2 { return 0 }; return a + b }\n"}
			},
			steps: []string{"✗reference solution passes all tests"},
		},
		{
			name: "visible tests that need the hidden files",
			change: func(in *GateInput) {
				in.Tests = Files{"sum_test.go": test(`func TestSum(t *testing.T) { check(t, 1, 2, 3) }`)}
				in.HiddenTests = Files{"sum_hidden_test.go": test(`func check(t *testing.T, a, b, want int) { if Sum(a, b) != want { t.Fatal("wrong") } }
func TestNegative(t *testing.T) { check(t, -1, -2, -3) }`)}
			},
			steps: []string{"reference solution passes all tests", "✗reference solution passes the visible tests on their own"},
		},
		{
			name:   "hidden files without tests",
			change: func(in *GateInput) { in.HiddenTests = Files{"helpers_test.go": "package task\n"} },
			steps:  []string{"reference solution passes all tests", "✗visible and hidden tests have their own names"},
		},
		{
			// The gate goes on after a bad starter, so every mutant is checked too.
			name: "a solved starter and weak mutants",
			change: func(in *GateInput) {
				in.Starter = in.Reference
				in.Mutants = []Mutant{
					{Name: "same as the reference", Files: Files{"sum.go": "package task\n\nfunc Sum(x, y int) int { return y + x }\n"}},
					{Name: "doesn't compile", Files: Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a + }\n"}},
					{Name: "ignores b", Files: Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a }\n"}},
				}
			},
			steps: []string{
				"reference solution passes all tests",
				"visible tests pass on their own",
				"✗starter code fails at least one test",
				`✗mutant "same as the reference" is caught`,
				`✗mutant "doesn't compile" is caught`,
				`mutant "ignores b" is caught`,
			},
		},
		{
			name:   "a starter that doesn't compile",
			change: func(in *GateInput) { in.Starter = Files{"sum.go": "package task\n"} },
			steps: []string{
				"reference solution passes all tests",
				"visible tests pass on their own",
				"✗starter code compiles",
				`mutant "ignores b" is caught`,
				`mutant "off by one" is caught`,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			in := good()
			c.change(&in)
			rep := gateLocal(t, in)
			if rep.Passed != c.passed {
				t.Errorf("passed %v, want %v:\n%s", rep.Passed, c.passed, rep)
			}
			if len(rep.Steps) != len(c.steps) {
				t.Fatalf("%d steps, want %d:\n%s", len(rep.Steps), len(c.steps), rep)
			}
			for i, want := range c.steps {
				name, failed := strings.CutPrefix(want, "✗")
				if s := rep.Steps[i]; s.OK == failed || !strings.HasPrefix(s.Name, name) {
					t.Errorf("step %d is %q (ok %v), want %q:\n%s", i, s.Name, s.OK, want, rep)
				}
			}
		})
	}
}

// The gate saves the names the reference run reported, telling hidden tests
// apart by the visible-only run.
func TestGateNamesTests(t *testing.T) {
	in := GateInput{
		Toolchain:   goToolchain,
		Starter:     Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return 0 }\n"},
		Reference:   Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a + b }\n"},
		Tests:       Files{"sum_test.go": test("func TestSum(t *testing.T) { t.Run(\"case\", func(t *testing.T) {}) }\nfunc TestZero(t *testing.T) {}")},
		HiddenTests: Files{"edge_test.go": test(`func TestEdge(t *testing.T) { if Sum(1, 1) != 2 { t.Fatal() } }`)},
		Mutants: []Mutant{
			{Name: "a", Files: Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a }\n"}},
			{Name: "b", Files: Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return b }\n"}},
		},
	}
	rep := gateLocal(t, in)
	if !rep.Passed {
		t.Fatalf("gate failed:\n%s", rep)
	}
	if strings.Join(rep.Names.Visible, " ") != "TestSum TestZero" || strings.Join(rep.Names.Hidden, " ") != "TestEdge" {
		t.Errorf("names %+v", rep.Names)
	}
}

// Go can't have two tests of one name, but JUnit frameworks can, in
// different files or classes. Results would then be ambiguous.
func TestSplitNamesRejectsDuplicates(t *testing.T) {
	all := []TestResult{{Name: "test_x"}, {Name: "test_x"}, {Name: "test_y"}}
	if _, problem := splitNames(all, all[:1]); !strings.Contains(problem, `Two tests are called "test_x"`) {
		t.Errorf("problem %q", problem)
	}
}

// gateLocal runs the gate on the host.
func gateLocal(t *testing.T, in GateInput) GateReport {
	t.Helper()
	needGo(t)
	rep, err := (&Runner{Engine: "local"}).Gate(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}
