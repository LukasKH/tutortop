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

// Callers that need the same image share one build, and the caller that
// started it can give up without stopping it for the others.
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
	calls := func() string { b, _ := os.ReadFile(log); return string(b) }

	// The first caller starts the build, then gives up.
	quitter, cancel := context.WithCancel(t.Context())
	quit := make(chan error, 1)
	go func() { quit <- r.Prepare(quitter, tc) }()
	for !strings.Contains(calls(), "build") {
		time.Sleep(10 * time.Millisecond)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { errs[i] = r.Prepare(t.Context(), tc) })
	}
	cancel()
	if err := <-quit; !errors.Is(err, context.Canceled) {
		t.Fatalf("the caller that gave up got %v", err)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("errors: %v", errs)
	}
	if got := strings.Count(calls(), "build"); got != 1 {
		t.Fatalf("%d builds, want 1:\n%s", got, calls())
	}
	before := calls()
	if err := r.Prepare(t.Context(), tc); err != nil {
		t.Fatal(err)
	}
	if calls() != before {
		t.Fatalf("a prepared image was checked again:\n%s", calls())
	}
}
