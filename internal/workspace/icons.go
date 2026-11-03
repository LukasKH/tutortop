package workspace

import (
	"fmt"
	"slices"
	"strings"
)

// Icons are the pictures the app can draw for a workspace: languages and
// tools by their logo, other topics by a simple glyph. The frontend draws
// each one (frontend/src/components/topic-icon.tsx); keep the two lists in step.
var Icons = []string{
	// languages
	"go", "python", "rust", "typescript", "javascript", "c", "cplusplus", "java", "kotlin", "swift",
	"csharp", "haskell", "elixir", "ruby", "php", "lua", "zig", "ocaml", "r", "julia", "scala", "clojure", "gleam", "bash",
	// tools and libraries
	"sqlite", "postgresql", "redis", "docker", "kubernetes", "git", "linux", "htmx", "react", "nodejs", "wasm", "numpy", "pandas",
	// glyphs for everything else
	"terminal", "database", "network", "braces", "regex", "cpu", "chart", "lock", "tree", "book",
}

// SetIcon picks the workspace's icon from Icons; "" clears it.
func (w *Workspace) SetIcon(name string) error {
	if name != "" && !slices.Contains(Icons, name) {
		return fmt.Errorf("unknown icon %q; pick one of: %s", name, strings.Join(Icons, ", "))
	}
	return w.updateMeta(func(m *Meta) bool { m.Icon = name; return true })
}
