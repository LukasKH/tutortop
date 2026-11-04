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

// Close stops a process that doesn't exit when its stdin closes.
func TestProcessCloseStops(t *testing.T) {
	p, err := startProcess(t.Context(), t.TempDir(), nil, "sleep", "60")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = p.Close()
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Close took %v", d)
	}
}
