package agent

import (
	"io"
	"testing"
	"time"
)

// The last line a CLI prints before it exits, such as its result or error,
// must still reach a reader that's busy with the line before.
func TestProcessKeepsOutputAfterExit(t *testing.T) {
	p, err := startProcess(t.Context(), t.TempDir(), nil, "sh", "-c", `printf 'first\nlast\n'`)
	if err != nil {
		t.Fatal(err)
	}
	first := make([]byte, len("first\n"))
	if _, err := io.ReadFull(p.stdout, first); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // the process exits meanwhile
	rest, err := io.ReadAll(p.stdout)
	if err != nil || string(rest) != "last\n" {
		t.Fatalf("read %q, %v; want the last line", rest, err)
	}
}

// Close returns, and the process stops, even when its output is never read.
func TestProcessCloseUnread(t *testing.T) {
	p, err := startProcess(t.Context(), t.TempDir(), nil, "sh", "-c", "yes")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // the pipe fills up
	_ = p.Close()
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		t.Fatal("the process is still running 3s after Close")
	}
}
