// Package text has small string helpers shared across packages.
package text

import "unicode/utf8"

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
