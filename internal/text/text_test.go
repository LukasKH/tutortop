package text

import "testing"

func TestTruncateKeepsRunesWhole(t *testing.T) {
	if got := Truncate("héllo", 2); got != "h…" {
		t.Fatalf("Truncate = %q", got)
	}
	if got := Tail("héllo", 4); got != "…llo" {
		t.Fatalf("Tail = %q", got)
	}
	if got := Truncate("abc", 5); got != "abc" {
		t.Fatalf("Truncate short = %q", got)
	}
}
