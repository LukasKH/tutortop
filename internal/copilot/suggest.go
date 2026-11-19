package copilot

import (
	"strings"
	"unicode/utf16"
)

// oneLine turns Copilot's suggestion into one for the rest of the cursor's
// line, so a learner gets help with syntax but writes the solution's lines
// themselves. line is the cursor's line. It reports false for a suggestion
// that adds nothing to the line, such as one that only starts the next line.
func oneLine(item completionItem, cursor Position, line string) (Suggestion, bool) {
	r := item.Range
	if r.Start.Line != cursor.Line || r.End.Line != cursor.Line {
		return Suggestion{}, false
	}
	text, _, _ := strings.Cut(item.InsertText, "\n")
	text = strings.TrimSuffix(text, "\r")
	units := utf16.Encode([]rune(line))
	if r.Start.Character > r.End.Character || r.End.Character > len(units) {
		return Suggestion{}, false
	}
	replaced := string(utf16.Decode(units[r.Start.Character:r.End.Character]))
	if text == replaced {
		return Suggestion{}, false
	}
	return Suggestion{Text: text, Range: r}, true
}

// lineAt returns line n of text, counted from 0.
func lineAt(text string, n int) string {
	for range n {
		_, rest, ok := strings.Cut(text, "\n")
		if !ok {
			return ""
		}
		text = rest
	}
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSuffix(line, "\r")
}
