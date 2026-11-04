package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Callers that need the same image share one build, and one that gives up
// doesn't stop it for the others.
func TestPrepareSharesBuild(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	engine := filepath.Join(dir, "engine")
	script := "#!/bin/sh\necho \"$1\" >> " + log + "\ncase \"$1\" in image) exit 1 ;; build) cat >/dev/null; sleep 0.5 ;; esac\n"
	if err := os.WriteFile(engine, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Runner{Engine: engine, prepared: map[string]bool{}}
	tc := Toolchain{Name: "Go", Image: "tutortop-test:1", Dockerfile: "FROM scratch"}

	quitter, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Go(func() {
			ctx := t.Context()
			if i == 0 {
				ctx = quitter
			}
			errs[i] = r.Prepare(ctx, tc)
		})
	}
	wg.Wait()
	if !errors.Is(errs[0], context.DeadlineExceeded) || errs[1] != nil || errs[2] != nil {
		t.Fatalf("errors: %v", errs)
	}
	calls, _ := os.ReadFile(log)
	if got := strings.Count(string(calls), "build"); got != 1 {
		t.Fatalf("%d builds, want 1:\n%s", got, calls)
	}
	if err := r.Prepare(t.Context(), tc); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(log); len(again) != len(calls) {
		t.Fatalf("a prepared image was checked again:\n%s", again)
	}
}
