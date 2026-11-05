package workspace

import (
	"fmt"
	"slices"
	"strings"
)

// Icons lists the icon names a workspace can use.
func Icons() []string { return slices.Clone(icons) }

// ValidIcon reports whether name is one of Icons, or "" for none.
func ValidIcon(name string) bool { return name == "" || slices.Contains(icons, name) }

// SetIcon picks the workspace's icon from Icons; "" clears it.
func (w *Workspace) SetIcon(name string) error {
	if !ValidIcon(name) {
		return fmt.Errorf("unknown icon %q; pick one of: %s", name, strings.Join(icons, ", "))
	}
	return w.updateMeta(func(m *Meta) bool { m.Icon = name; return true })
}

// icons are the pictures the app can draw for a workspace: languages and
// tools by their logo, other topics by a simple glyph. The frontend draws
// each one (frontend/src/components/topic-icon.tsx); keep the two lists in step.
var icons = []string{
	// languages
	"go", "python", "rust", "typescript", "javascript", "c", "cplusplus", "java", "kotlin", "swift",
	"csharp", "haskell", "elixir", "ruby", "php", "lua", "zig", "ocaml", "r", "julia", "scala", "clojure", "gleam", "bash",
	// tools and libraries
	"sqlite", "postgresql", "redis", "docker", "kubernetes", "git", "linux", "htmx", "react", "nodejs", "wasm", "numpy", "pandas",
	// glyphs for everything else
	"terminal", "database", "network", "braces", "regex", "cpu", "chart", "lock", "tree", "book",
}
