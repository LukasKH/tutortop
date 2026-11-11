package sandbox

import (
	"bytes"
	"encoding/json"
	"strings"
)

// goTestEvent is one line of `go test -json` output.
type goTestEvent struct {
	Action goAction
	Test   string
	Output string
}

// goAction is a `go test -json` event's action, those the app reads.
type goAction string

const (
	goOutput      goAction = "output"
	goBuildOutput goAction = "build-output"
	goPass        goAction = "pass"
	goFail        goAction = "fail"
)

// parseGoTestJSON reads `go test -json` output. Build failures produce no test
// events, so ok=false tells the caller the code didn't compile.
func parseGoTestJSON(stdout string, hidden map[string]bool) ([]TestResult, bool) {
	var order []string
	results := map[string]*TestResult{}
	output := map[string]*strings.Builder{}
	sawEvents, packageFailed := false, false

	var ev goTestEvent
	for line := range strings.Lines(stdout) {
		ev = goTestEvent{} // or the previous line's fields leak into this one
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Test == "" {
			packageFailed = packageFailed || ev.Action == goFail
			continue
		}
		sawEvents = true
		// Subtests ("TestX/case") are reported under their parent's output.
		top, _, _ := strings.Cut(ev.Test, "/")
		if _, ok := results[top]; !ok {
			results[top] = &TestResult{Name: top, Hidden: hidden[top]}
			output[top] = &strings.Builder{}
			order = append(order, top)
		}
		switch ev.Action {
		case goOutput:
			if !strings.HasPrefix(strings.TrimSpace(ev.Output), "=== ") {
				output[top].WriteString(ev.Output)
			}
		case goPass, goFail:
			if ev.Test == top {
				results[top].Passed = ev.Action == goPass
			}
		case goBuildOutput:
			// Build output has no test; it's shown with the whole output.
		}
	}
	out := make([]TestResult, 0, len(order))
	allPassed := true
	for _, name := range order {
		r := results[name]
		if !r.Passed {
			r.Message = trimMessage(output[name].String())
			allPassed = false
		}
		out = append(out, *r)
	}
	// Every test passed but the test program still failed, e.g. a failing
	// TestMain or an os.Exit after the tests. That isn't a pass.
	if sawEvents && allPassed && packageFailed {
		out = append(out, TestResult{Name: "(test program)", Message: "The test program failed outside any single test. See the output."})
	}
	return out, sawEvents
}

// humanGoOutput turns `go test -json` lines back into readable text.
func humanGoOutput(s string) string {
	var b bytes.Buffer
	var ev goTestEvent
	for line := range strings.Lines(s) {
		ev = goTestEvent{} // or the previous line's fields leak into this one
		if json.Unmarshal([]byte(line), &ev) == nil && ev.Action != "" {
			if ev.Action == goOutput || ev.Action == goBuildOutput {
				b.WriteString(ev.Output)
			}
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}
