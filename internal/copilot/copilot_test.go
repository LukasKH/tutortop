package copilot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Setup returns at once when the server can't be asked, without waiting for
// GitHub to say whether there's a newer release.
func TestSetupDoesNotWaitForGitHubOnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(binaryPath(dir), []byte("server"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "version"), []byte("1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewService(dir, nil)
	s.installing = true // so status fails without starting the server

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
			t.Fatal("Setup succeeded while installing")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Setup waited for GitHub after the server failed")
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
