package copilot

import "testing"

func TestOneLine(t *testing.T) {
	at := func(line, char int) Position { return Position{Line: line, Character: char} }
	span := func(line, from, to int) Range { return Range{Start: at(line, from), End: at(line, to)} }
	tests := []struct {
		name   string
		item   completionItem
		cursor Position
		line   string
		want   string // "" for no suggestion
	}{
		{
			name:   "keeps the rest of the cursor's line",
			item:   completionItem{InsertText: "\tfor i := range n {\n\t\tsum += i\n\t}", Range: span(3, 0, 5)},
			cursor: at(3, 5),
			line:   "\tfor ",
			want:   "\tfor i := range n {",
		},
		{
			name:   "a one-line suggestion stays whole",
			item:   completionItem{InsertText: "return a + b", Range: span(0, 0, 3)},
			cursor: at(0, 3),
			line:   "ret",
			want:   "return a + b",
		},
		{
			name:   "Windows line endings",
			item:   completionItem{InsertText: "x := 1\r\ny := 2", Range: span(0, 0, 0)},
			cursor: at(0, 0),
			line:   "",
			want:   "x := 1",
		},
		{
			name:   "drops a suggestion that only starts the next line",
			item:   completionItem{InsertText: "sum := 0\n\treturn sum", Range: span(1, 0, 8)},
			cursor: at(1, 8),
			line:   "sum := 0",
		},
		{
			name:   "counts UTF-16 characters, as the editor does",
			item:   completionItem{InsertText: `s := "😀" + name`, Range: span(0, 0, 8)},
			cursor: at(0, 8),
			line:   `s := "😀"`,
			want:   `s := "😀" + name`,
		},
		{
			name:   "drops a range on another line",
			item:   completionItem{InsertText: "x", Range: span(2, 0, 0)},
			cursor: at(1, 0),
			line:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := oneLine(tt.item, tt.cursor, tt.line)
			if !ok {
				got.Text = ""
			}
			if got.Text != tt.want {
				t.Errorf("oneLine = %q, want %q", got.Text, tt.want)
			}
		})
	}
}

func TestLineAt(t *testing.T) {
	text := "a\r\nb\nc"
	for n, want := range []string{"a", "b", "c", ""} {
		if got := lineAt(text, n); got != want {
			t.Errorf("lineAt(%d) = %q, want %q", n, got, want)
		}
	}
}
