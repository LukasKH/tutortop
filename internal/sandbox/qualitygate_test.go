package sandbox

import (
	"reflect"
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
			Toolchain:   builtins["go"],
			Starter:     Files{"sum.go": starter},
			Reference:   Files{"sum.go": reference},
			Tests:       Files{"sum_test.go": test(`func TestSum(t *testing.T) { if Sum(1, 2) != 3 { t.Fatal("Sum(1, 2) != 3") } }`)},
			HiddenTests: Files{"sum_hidden_test.go": test(`func TestNegative(t *testing.T) { if Sum(-1, -2) != -3 { t.Fatal("Sum(-1, -2) != -3") } }`)},
		}
	}
	cases := []struct {
		name   string
		change func(in *GateInput)
		passed bool
		steps  []string   // every step, failed ones marked with a leading ✗
		names  *TestNames // if set, the names the gate saves
	}{
		{
			name:   "a sound task",
			change: func(*GateInput) {},
			passed: true,
			steps: []string{
				"reference solution passes all tests",
				"visible tests pass on their own (1 visible, 1 hidden)",
				"starter code fails at least one test",
			},
			names: &TestNames{Visible: []string{"TestSum"}, Hidden: []string{"TestNegative"}},
		},
		{
			name: "missing parts are reported before anything runs",
			change: func(in *GateInput) {
				in.Tests, in.HiddenTests = nil, nil
			},
			steps: []string{"✗has visible tests", "✗has hidden tests"},
		},
		{
			name: "a wrong reference",
			change: func(in *GateInput) {
				in.Reference = Files{"sum.go": "package task\n\nfunc Sum(a, b int) int { return a - b }\n"}
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
			// Go can't have two tests of one name, but JUnit frameworks can,
			// in different files or classes. Results would then be ambiguous.
			name: "the same test name in visible and hidden files",
			change: func(in *GateInput) {
				test := "from calc import add\n\n\ndef test_add():\n    assert add(1, 2) == 3\n"
				*in = GateInput{
					Toolchain:   builtins["python"],
					Starter:     Files{"calc.py": "def add(a, b):\n    return 0\n"},
					Reference:   Files{"calc.py": "def add(a, b):\n    return a + b\n"},
					Tests:       Files{"test_calc.py": test},
					HiddenTests: Files{"test_calc_hidden.py": test},
				}
			},
			steps: []string{"reference solution passes all tests", "✗visible and hidden tests have their own names"},
		},
		{
			name:   "a solved starter",
			change: func(in *GateInput) { in.Starter = in.Reference },
			steps: []string{
				"reference solution passes all tests",
				"visible tests pass on their own",
				"✗starter code fails at least one test",
			},
		},
		{
			name:   "a starter that doesn't compile",
			change: func(in *GateInput) { in.Starter = Files{"sum.go": "package task\n"} },
			steps: []string{
				"reference solution passes all tests",
				"visible tests pass on their own",
				"✗starter code compiles",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			in := good()
			c.change(&in)
			rep := runGate(t, in)
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
			if c.names != nil && !reflect.DeepEqual(rep.Names, *c.names) {
				t.Errorf("names %+v, want %+v", rep.Names, *c.names)
			}
		})
	}
}

// runGate runs the quality gate in the sandbox.
func runGate(t *testing.T, in GateInput) GateReport {
	t.Helper()
	rep, err := testRunner(t).Gate(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}
