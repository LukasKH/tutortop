package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
	r := &Runner{Engine: Engine(engine), prepared: map[string]uint64{}}
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

// A workspace's runs keep the toolchain's cache in a volume of their own, the
// quality gate's runs start clean, and deleting a workspace's cache leaves
// other workspaces' volumes alone.
func TestCacheVolumes(t *testing.T) {
	mine, other := cacheVolumePrefix("ws-a")+"go", cacheVolumePrefix("ws-b")+"go"
	r, calls := fakeEngine(t, []string{mine, other}, nil)
	run := func(cache string) string {
		before := calls()
		if _, err := r.Run(t.Context(), Job{Toolchain: builtins["go"], Cache: cache}); err != nil {
			t.Fatal(err)
		}
		return strings.TrimPrefix(calls(), before)
	}

	if got := run("ws-a"); !strings.Contains(got, " -v "+mine+":/home/runner/.cache/go-build ") {
		t.Errorf("a workspace's run doesn't mount its cache:\n%s", got)
	}
	if got := run(""); strings.Contains(got, " -v ") {
		t.Errorf("a run without a cache, as the quality gate's, mounts a volume:\n%s", got)
	}
	if err := r.RemoveCache(t.Context(), "ws-a"); err != nil {
		t.Fatal(err)
	}
	if got := calls(); !strings.Contains(got, "volume rm -f "+mine+"\n") || strings.Contains(got, "rm -f "+other) {
		t.Errorf("removing ws-a's cache, want only %s removed:\n%s", mine, got)
	}
}

// A check's container gets the memory its toolchain allows.
func TestRunMemory(t *testing.T) {
	r, calls := fakeEngine(t, nil, nil)
	raised := rSpec()
	raised.MemoryMB = 2048
	custom, err := raised.Toolchain()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		tc   Toolchain
		want string
	}{
		{builtins["go"], " --memory 512m "},
		{custom, " --memory 2048m "},
	}
	for _, c := range cases {
		before := calls()
		if _, err := r.Run(t.Context(), Job{Toolchain: c.tc}); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimPrefix(calls(), before); !strings.Contains(got, c.want) {
			t.Errorf("%s's run, want%s:\n%s", c.tc.Name, c.want, got)
		}
	}
}

// Images of the toolchains in use stay, whichever engine listed them, and so
// do images from a registry that only share the name; the rest go, including
// older builds of a language still in use. A removed image is built again
// when it's next needed.
func TestRemoveUnusedImages(t *testing.T) {
	goTC, r := builtins["go"], customToolchain(t)
	runner, calls := fakeEngine(t, nil, []string{
		goTC.Image,
		"tutortop-go:000000000000",
		"localhost/" + r.Image,
		"docker.io/someone/tutortop-r:1",
	})
	if err := runner.Prepare(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	if err := runner.RemoveUnusedImages(t.Context(), []Toolchain{goTC}); err != nil {
		t.Fatal(err)
	}
	var removed []string
	for line := range strings.Lines(calls()) {
		if image, ok := strings.CutPrefix(strings.TrimSpace(line), "image rm "); ok {
			removed = append(removed, image)
		}
	}
	if want := []string{"tutortop-go:000000000000", "localhost/" + r.Image}; !slices.Equal(removed, want) {
		t.Errorf("removed %q, want %q", removed, want)
	}
	if err := runner.Prepare(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(calls(), "image inspect "+r.Image+"\n"); n != 2 {
		t.Errorf("looked up %s %d times, want 2: removing it should make the next Prepare build it again", r.Image, n)
	}
}

// fakeEngine is a container engine that logs each call's arguments as a line,
// lists the given volumes and images, and succeeds at everything else.
func fakeEngine(t *testing.T, volumes, images []string) (r *Runner, calls func() string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	engine := filepath.Join(dir, "engine")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\n" +
		"case \"$1 $2\" in\n" +
		"\"volume ls\") printf '%s\\n' " + strings.Join(volumes, " ") + " ;;\n" +
		"\"image ls\") printf '%s\\n' " + strings.Join(images, " ") + " ;;\n" +
		"run*) cat >/dev/null ;;\n" +
		"esac\n"
	if err := os.WriteFile(engine, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r = &Runner{Engine: Engine(engine), prepared: map[string]uint64{}}
	return r, func() string { b, _ := os.ReadFile(log); return string(b) }
}

// If a prepared image disappears externally (e.g. `docker image prune` or
// another instance's cleanup), Run rebuilds it once and retries — even when
// two runs hit the missing image at the same time. A compile error on a
// present image doesn't trigger a rebuild.
func TestRunRebuildsRemovedImage(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	present := filepath.Join(dir, "present")
	engine := filepath.Join(dir, "engine")
	// Fake engine: image exists iff `present` exists; `build` creates `present`;
	// `run` prints one passing go-json test when `present` exists, else fails
	// with Docker's missing-image message.
	script := `#!/bin/sh
echo "$1 $2" >> ` + log + `
case "$1" in
image)
	test -f ` + present + `
	;;
build)
	cat >/dev/null
	sleep 0.1
	touch ` + present + `
	;;
run)
	cat >/dev/null
	if [ ! -f ` + present + ` ]; then
		echo "Unable to find image locally" >&2
		exit 125
	fi
	if [ -f ` + filepath.Join(dir, "compile-error") + ` ]; then
		echo "syntax error" >&2
		exit 1
	fi
	printf '{"Action":"pass","Package":"task","Test":"TestOK"}\n'
	;;
esac
`
	if err := os.WriteFile(engine, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Runner{Engine: Engine(engine), prepared: map[string]uint64{}}
	tc := builtins["go"]
	calls := func() string { b, _ := os.ReadFile(log); return string(b) }

	// Prepare once so the image is cached as prepared, then delete it externally.
	if err := r.Prepare(t.Context(), tc); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(present); err != nil {
		t.Fatal(err)
	}

	// Two concurrent runs hit the missing image at once; both should succeed
	// after sharing one rebuild.
	var wg sync.WaitGroup
	results := make([]Result, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Go(func() {
			results[i], errs[i] = r.Run(t.Context(), Job{Toolchain: tc})
		})
	}
	wg.Wait()
	for i := range results {
		if errs[i] != nil || results[i].Status != StatusPassed {
			t.Fatalf("run %d: res=%+v err=%v", i, results[i], errs[i])
		}
	}
	// One build from the initial Prepare, one from the external removal.
	if got := strings.Count(calls(), "build -t"); got != 2 {
		t.Fatalf("%d builds, want 2:\n%s", got, calls())
	}

	// A compile error while the image is present returns StatusError without rebuilding.
	if err := os.WriteFile(filepath.Join(dir, "compile-error"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	before := strings.Count(calls(), "build -t")
	res, err := r.Run(t.Context(), Job{Toolchain: tc})
	if err != nil || res.Status != StatusError {
		t.Fatalf("compile error: res=%+v err=%v, want StatusError", res, err)
	}
	if got := strings.Count(calls(), "build -t"); got != before {
		t.Fatalf("compile error triggered a rebuild:\n%s", calls())
	}
}
