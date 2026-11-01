//go:build live

package agent

import (
	"context"
	"testing"
	"time"
)

// TestLiveStop checks that interrupting a turn ends it cleanly (no error)
// and that the session keeps working afterwards.
func TestLiveStop(t *testing.T) {
	ctx := context.Background()
	sess, err := Start(ctx, "claude", Options{Cwd: t.TempDir(), MCPName: "probe", MCPURL: "http://127.0.0.1:1/mcp", Instructions: "Be brief."}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	go func() { time.Sleep(6 * time.Second); sess.Cancel() }()
	if err := sess.Prompt(ctx, "Write a 400 word essay about goroutines."); err != nil {
		t.Fatalf("interrupted prompt returned an error: %v", err)
	}
	if err := sess.Prompt(ctx, "Say OK."); err != nil {
		t.Fatalf("prompt after interrupt: %v", err)
	}
}
