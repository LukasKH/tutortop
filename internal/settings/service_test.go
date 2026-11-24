package settings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/sandbox"
)

// Callers asking for the same models at once share one probe of the CLI, and
// one caller giving up doesn't cut it short for the others.
func TestModelsShareOneProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI is a shell script")
	}
	dir := t.TempDir()
	asked := filepath.Join(dir, "asked")
	release := filepath.Join(dir, "release")
	bin := filepath.Join(dir, "codex")
	script := "#!/bin/sh\necho >> '" + asked + "'\nwhile [ ! -f '" + release + "' ]; do sleep 0.01; done\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	p, _ := agent.Lookup(string(agent.Codex))
	c := newModelCache()

	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() {
		_, err := c.list(ctx, p, bin)
		first <- err
	}()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(asked); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the CLI was never asked")
		}
	}
	others := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := c.list(t.Context(), p, bin)
			others <- err
		}()
	}
	time.Sleep(50 * time.Millisecond) // let both waiters enter DoChan before cancelling first
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Errorf("the caller that gave up got %v, want context.Canceled", err)
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-others; err == nil || errors.Is(err, context.Canceled) {
			t.Errorf("a waiting caller got %v, want the probe's own error", err)
		}
	}
	b, err := os.ReadFile(asked)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n != 1 {
		t.Errorf("the CLI was asked %d times, want 1", n)
	}
}

// A check that finds a problem evicts cached models for that CLI so Models
// asks again instead of serving the old list.
func TestCheckEvictsCachedModelsOnProblem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI is a shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho broken >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(dir, "settings.yaml"))
	if err := store.Save(Prefs{Provider: "codex", Agents: map[string]AgentSetup{"codex": {Path: bin}}}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, nil, dir)
	p, _ := agent.Lookup(string(agent.Codex))
	svc.models.put(p, bin, []agent.Model{{ID: "old", Name: "Old"}})

	got, err := svc.Check(t.Context(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if got.Problem == "" {
		t.Fatal("Check reported no problem for a broken CLI")
	}
	if _, err := svc.Models(t.Context(), "codex"); err == nil {
		t.Error("Models succeeded from cache after Check found a problem")
	}
}

// Environment looks up each agent using the custom path saved in settings.
func TestEnvironmentUsesCustomAgentPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI is a shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "my-codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(dir, "settings.yaml"))
	if err := store.Save(Prefs{Provider: "codex", Agents: map[string]AgentSetup{"codex": {Path: bin}}}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, &sandbox.Runner{}, dir)
	env := svc.Environment()
	for _, p := range env.Providers {
		if p.ID == string(agent.Codex) {
			if !p.Installed || p.Path != bin {
				t.Fatalf("codex status = %+v, want installed at %q", p, bin)
			}
			return
		}
	}
	t.Fatal("codex missing from Environment.Providers")
}
