package copilot

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/events"
	"github.com/sourcegraph/jsonrpc2"
)

// installed puts a fake language server, which can't start, in a new folder.
func installed(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(binaryPath(dir), []byte("server"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "version"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Setup returns at once when the server can't be asked, without waiting for
// GitHub to say whether there's a newer release.
func TestSetupDoesNotWaitForGitHubOnError(t *testing.T) {
	s := NewService(installed(t, "1.0.0"), nil) // a server that fails to start

	// A GitHub lookup that never answers, which newer joins.
	block := make(chan struct{})
	defer close(block)
	s.lookups.DoChan("latest", func() (any, error) {
		<-block
		return "9.9.9", nil
	})

	done := make(chan error, 1)
	go func() {
		_, err := s.Setup(t.Context())
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Setup succeeded with a server that can't start")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Setup waited for GitHub after the server failed")
	}
}

// A Settings screen opened during an install shows it running, so it isn't
// started twice, and shows the version being replaced.
func TestSetupWhileInstalling(t *testing.T) {
	s := NewService(installed(t, "1.0.0"), nil)
	s.installing = true
	got, err := s.Setup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if want := (Setup{Installing: true, Installed: true, Version: "1.0.0"}); got != want {
		t.Errorf("Setup while installing = %+v, want %+v", got, want)
	}
}

// Install tells the frontend when it ends, also when it fails, for a Settings
// screen opened after the one that started it.
func TestInstallEmitsWhenItEnds(t *testing.T) {
	var got []events.Install
	events.SetEmitter(func(name string, data any) {
		if name == string(events.CopilotInstalled) {
			got = append(got, data.(events.Install))
		}
	})
	t.Cleanup(func() { events.SetEmitter(nil) })

	s := NewService(t.TempDir(), nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // so the download fails at once
	if _, err := s.Install(ctx); err == nil {
		t.Fatal("Install succeeded without a connection")
	}
	if len(got) != 1 || got[0].Error == "" {
		t.Fatalf("events after a failed install = %+v, want one with its error", got)
	}
	if setup, err := s.Setup(t.Context()); err != nil || setup.Installing {
		t.Errorf("Setup after the install = %+v, %v; want it no longer installing", setup, err)
	}
}

func TestNewerOffersOnlyNewerReleases(t *testing.T) {
	for _, c := range []struct {
		latest, installed, want string
	}{
		{"1.392.0", "1.391.0", "1.392.0"},
		{"1.10.0", "1.9.0", "1.10.0"},
		{"1.391.0", "1.391.0", ""},
		{"1.9.0", "1.10.0", ""}, // a downgrade
		{"", "1.391.0", ""},     // GitHub didn't answer
		{"nightly", "1.391.0", ""},
	} {
		s := NewService(t.TempDir(), nil)
		s.latest, s.latestAt = c.latest, time.Now() // fresh, so GitHub isn't asked
		if got := s.newer(t.Context(), c.installed); got != c.want {
			t.Errorf("newer with %q installed and %q latest = %q, want %q", c.installed, c.latest, got, c.want)
		}
	}
}

// Cancelled inline completion requests (from rapid typing or LSP RequestCancelled)
// return an empty completion instead of an error, avoiding 422s and unhandled
// CancellablePromise rejections in the frontend.
func TestCompleteReturnsEmptyOnCancel(t *testing.T) {
	req := CompletionRequest{URI: "file:///t1/work/main.go", LanguageID: "go", Text: "package main\n"}

	// Already cancelled before calling Complete.
	s := NewService(t.TempDir(), nil)
	preCtx, preCancel := context.WithCancel(t.Context())
	preCancel()
	got, err := s.Complete(preCtx, req)
	if err != nil || got.Suggestions == nil || len(got.Suggestions) != 0 {
		t.Fatalf("pre-cancelled Complete = (%+v, %v), want empty slice and nil error", got, err)
	}

	// Cancelled while waiting on textDocument/inlineCompletion.
	received := make(chan struct{}, 1)
	s.server = fakeServer(t, func(_ context.Context, _ *jsonrpc2.Conn, r *jsonrpc2.Request) {
		if r.Method == "textDocument/inlineCompletion" {
			received <- struct{}{}
		}
	})
	midCtx, midCancel := context.WithCancel(t.Context())
	type result struct {
		c   Completion
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := s.Complete(midCtx, req)
		done <- result{c, err}
	}()
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("server never received inlineCompletion")
	}
	midCancel()
	select {
	case r := <-done:
		if r.err != nil || r.c.Suggestions == nil || len(r.c.Suggestions) != 0 {
			t.Fatalf("mid-flight cancelled Complete = (%+v, %v), want empty slice and nil error", r.c, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Complete did not return after cancel")
	}

	// Server answers LSP RequestCancelled (-32800).
	s.server = fakeServer(t, func(ctx context.Context, conn *jsonrpc2.Conn, r *jsonrpc2.Request) {
		if r.Method == "textDocument/inlineCompletion" {
			_ = conn.ReplyWithError(ctx, r.ID, &jsonrpc2.Error{Code: codeRequestCancelled, Message: "cancelled"})
		}
	})
	got, err = s.Complete(t.Context(), req)
	if err != nil || got.Suggestions == nil || len(got.Suggestions) != 0 {
		t.Fatalf("LSP RequestCancelled Complete = (%+v, %v), want empty slice and nil error", got, err)
	}
}
