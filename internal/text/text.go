// Package text has small string helpers shared across packages.
package text

import (
	"strings"
	"unicode/utf8"
)

// List joins items as prose: "a", "a and b", "a, b and c".
func List(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// Truncate shortens s to at most n bytes, cutting at a rune boundary and
// adding "…" when something was removed.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

// Tail keeps the last n bytes of s, cutting at a rune boundary and adding
// "…" in front when something was removed.
func Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return "…" + s[i:]
}
